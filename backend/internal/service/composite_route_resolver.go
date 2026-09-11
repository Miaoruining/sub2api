package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type CompositeRouteResolver struct {
	repo                   CompositeModelRouteRepository
	groupRepo              GroupRepository
	modelOwnershipResolver CompositeModelOwnershipResolver
}

func NewCompositeRouteResolver(repo CompositeModelRouteRepository) *CompositeRouteResolver {
	return &CompositeRouteResolver{repo: repo}
}

func (r *CompositeRouteResolver) SetModelOwnershipResolver(resolver CompositeModelOwnershipResolver) {
	if r != nil {
		r.modelOwnershipResolver = resolver
	}
}

// SetGroupRepository injects group lookup used by source-group routes.  The
// setter keeps NewCompositeRouteResolver compatible with existing callers and
// lightweight tests that only exercise ordinary composite routes.
func (r *CompositeRouteResolver) SetGroupRepository(repo GroupRepository) {
	if r != nil {
		r.groupRepo = repo
	}
}

func (r *CompositeRouteResolver) Resolve(ctx context.Context, groupID int64, model, endpoint string) (CompositeRouteDecision, error) {
	model = strings.TrimSpace(model)
	endpoint = normalizeCompositeRouteEndpoint(endpoint)
	decision := CompositeRouteDecision{
		GroupID:     groupID,
		PublicModel: model,
		Endpoint:    endpoint,
	}
	if model == "" {
		decision.Reason = "model is required"
		return decision, nil
	}

	if r != nil && r.repo != nil && groupID > 0 {
		routes, err := r.repo.ListByGroup(ctx, groupID, true)
		if err != nil {
			return decision, fmt.Errorf("list composite routes: %w", err)
		}
		enabledRoutes := make([]CompositeModelRoute, 0, len(routes))
		hasSourceRoutes := false
		for _, route := range routes {
			hasSourceRoutes = hasSourceRoutes || route.SourceGroupID != nil
			if route.Enabled {
				enabledRoutes = append(enabledRoutes, route)
			}
		}
		if route, ok := matchCompositeRoute(enabledRoutes, model, endpoint); ok {
			upstreamModel := strings.TrimSpace(route.UpstreamModel)
			if upstreamModel == "" {
				upstreamModel = model
			}
			decision := CompositeRouteDecision{
				Matched:        true,
				Source:         CompositeRouteSourceExplicit,
				GroupID:        groupID,
				PublicModel:    model,
				TargetPlatform: route.TargetPlatform,
				UpstreamModel:  upstreamModel,
				Endpoint:       endpoint,
				Route:          &route,
			}
			if route.SourceGroupID != nil {
				if err := r.validateSourceGroupRoute(ctx, groupID, &route, model, upstreamModel); err != nil {
					decision.Matched = false
					decision.TargetPlatform = ""
					decision.UpstreamModel = ""
					decision.Route = nil
					decision.Reason = err.Error()
					return decision, nil
				}
				decision.SourceGroupID = route.SourceGroupID
				decision.SourceGroup = route.SourceGroup
			}
			return decision, nil
		}
		if hasSourceRoutes {
			decision.Source = CompositeRouteSourceExplicit
			decision.Reason = "no active source route matches this model and endpoint"
			return decision, nil
		}
	}

	if r != nil && r.modelOwnershipResolver != nil && groupID > 0 {
		ownership, err := r.modelOwnershipResolver(ctx, groupID, model)
		if err != nil {
			// A recognizable model can still use the existing detector when the
			// account catalog is temporarily unavailable. Unknown aliases cannot.
			if _, detectable := DetectModelPlatform(model); !detectable {
				return decision, fmt.Errorf("resolve account model ownership: %w", err)
			}
		} else if ownership.Ambiguous {
			decision.Reason = "model is exposed by multiple provider platforms"
			return decision, nil
		} else if ownership.Matched {
			platform := strings.TrimSpace(ownership.TargetPlatform)
			if !isConcreteRequestPlatform(platform) {
				decision.Reason = "account model ownership has no concrete target platform"
				return decision, nil
			}
			return CompositeRouteDecision{
				Matched:        true,
				Source:         CompositeRouteSourceAccount,
				GroupID:        groupID,
				PublicModel:    model,
				TargetPlatform: platform,
				UpstreamModel:  model,
				Endpoint:       endpoint,
			}, nil
		}
	}

	if platform, ok := DetectModelPlatform(model); ok {
		return CompositeRouteDecision{
			Matched:        true,
			Source:         CompositeRouteSourceDetector,
			GroupID:        groupID,
			PublicModel:    model,
			TargetPlatform: platform,
			UpstreamModel:  model,
			Endpoint:       endpoint,
		}, nil
	}
	decision.Reason = "no explicit route or built-in detector match"
	return decision, nil
}

// validateSourceGroupRoute validates the source-group boundary without
// recursively resolving the source group's own composite routes.  A source
// composite group may still expose concrete accounts or a plain one-level
// route, which is enough to establish the concrete provider for this route.
func (r *CompositeRouteResolver) validateSourceGroupRoute(ctx context.Context, outerGroupID int64, route *CompositeModelRoute, requestedModel, upstreamModel string) error {
	if route == nil || route.SourceGroupID == nil {
		return nil
	}
	if r == nil || r.groupRepo == nil {
		return fmt.Errorf("source group repository is not configured")
	}
	sourceGroupID := *route.SourceGroupID
	if sourceGroupID <= 0 {
		return fmt.Errorf("source_group_id must be positive")
	}
	if sourceGroupID == outerGroupID {
		return fmt.Errorf("source_group_id cannot reference the composite group itself")
	}
	outer, err := r.groupRepo.GetByIDLite(ctx, outerGroupID)
	if err != nil {
		return fmt.Errorf("load composite group: %w", err)
	}
	if outer == nil || outer.Platform != PlatformComposite {
		return fmt.Errorf("group %d is not a composite group", outerGroupID)
	}
	if outer.SubscriptionType != SubscriptionTypeStandard {
		return fmt.Errorf("composite group %d must use standard subscription type", outerGroupID)
	}
	source, err := r.groupRepo.GetByIDLite(ctx, sourceGroupID)
	if err != nil {
		return fmt.Errorf("load source group: %w", err)
	}
	if source == nil {
		return fmt.Errorf("source group %d not found", sourceGroupID)
	}
	if source.Status != StatusActive {
		return fmt.Errorf("source group %d is not active", sourceGroupID)
	}
	if source.IsExclusive {
		return fmt.Errorf("source group %d is exclusive", sourceGroupID)
	}
	if source.SubscriptionType != SubscriptionTypeStandard {
		return fmt.Errorf("source group %d must use standard subscription type", sourceGroupID)
	}

	if source.ModelAllowlistEnabled() && !source.ModelAllowlist.Allows(upstreamModel) {
		return fmt.Errorf("upstream model %q is not allowed by source group %d", upstreamModel, sourceGroupID)
	}

	targetPlatform := strings.TrimSpace(route.TargetPlatform)
	if !isConcreteRequestPlatform(targetPlatform) {
		return fmt.Errorf("target_platform must be a concrete provider")
	}
	if source.Platform != PlatformComposite {
		if source.Platform != targetPlatform {
			return fmt.Errorf("source group %d platform %q does not match target platform %q", sourceGroupID, source.Platform, targetPlatform)
		}
		route.SourceGroup = source
		return nil
	}

	if r.repo == nil {
		return fmt.Errorf("source composite group routes are unavailable")
	}
	sourceRoutes, err := r.repo.ListByGroup(ctx, sourceGroupID, true)
	if err != nil {
		return fmt.Errorf("list source composite routes: %w", err)
	}
	for _, sourceRoute := range sourceRoutes {
		if sourceRoute.SourceGroupID != nil {
			return fmt.Errorf("source composite group %d contains a nested source-group route", sourceGroupID)
		}
	}

	// Prefer the actual provider prefix in the upstream model.  This avoids
	// treating a model alias as a recursive route and works for the common
	// Gemini/CN provider IDs.
	if platform, ok := DetectModelPlatform(upstreamModel); ok {
		if platform != targetPlatform {
			return fmt.Errorf("upstream model %q resolves to platform %q, target platform is %q", upstreamModel, platform, targetPlatform)
		}
		route.SourceGroup = source
		return nil
	}

	// A plain source-composite route can establish the concrete provider for an
	// alias, but its target is deliberately not followed at request time.
	enabledRoutes := make([]CompositeModelRoute, 0, len(sourceRoutes))
	for _, sourceRoute := range sourceRoutes {
		if sourceRoute.Enabled {
			enabledRoutes = append(enabledRoutes, sourceRoute)
		}
	}
	sourceRoute, ok := matchCompositeRoute(enabledRoutes, upstreamModel, route.Endpoint)
	if !ok && requestedModel != upstreamModel {
		sourceRoute, ok = matchCompositeRoute(enabledRoutes, requestedModel, route.Endpoint)
	}
	if ok {
		if sourceRoute.SourceGroupID != nil {
			return fmt.Errorf("source composite group %d contains a nested source-group route", sourceGroupID)
		}
		if !isConcreteRequestPlatform(sourceRoute.TargetPlatform) || sourceRoute.TargetPlatform != targetPlatform {
			return fmt.Errorf("source composite route target platform %q does not match %q", sourceRoute.TargetPlatform, targetPlatform)
		}
		route.SourceGroup = source
		return nil
	}

	if r.modelOwnershipResolver != nil {
		ownership, ownershipErr := r.modelOwnershipResolver(ctx, sourceGroupID, upstreamModel)
		if ownershipErr == nil && ownership.Matched && !ownership.Ambiguous && ownership.TargetPlatform == targetPlatform {
			route.SourceGroup = source
			return nil
		}
	}
	return fmt.Errorf("cannot determine concrete platform for source composite group %d model %q", sourceGroupID, upstreamModel)
}

func matchCompositeRoute(routes []CompositeModelRoute, model, endpoint string) (CompositeModelRoute, bool) {
	if len(routes) == 0 {
		return CompositeModelRoute{}, false
	}

	type candidate struct {
		route          CompositeModelRoute
		matchStrength  int
		endpointWeight int
		prefixLen      int
	}
	candidates := make([]candidate, 0, len(routes))
	for _, route := range routes {
		route.Endpoint = normalizeCompositeRouteEndpoint(route.Endpoint)
		if route.Endpoint != endpoint && route.Endpoint != CompositeRouteEndpointAny {
			continue
		}
		route.MatchType = normalizeCompositeRouteMatchType(route.MatchType)
		publicModel := strings.TrimSpace(route.PublicModel)
		if publicModel == "" {
			continue
		}

		matchStrength := 0
		prefixLen := len(publicModel)
		switch route.MatchType {
		case CompositeRouteMatchExact:
			if publicModel != model {
				continue
			}
			matchStrength = 2
		case CompositeRouteMatchPrefix:
			if !strings.HasPrefix(model, publicModel) {
				continue
			}
			matchStrength = 1
		default:
			continue
		}
		endpointWeight := 0
		if route.Endpoint == endpoint {
			endpointWeight = 1
		}
		candidates = append(candidates, candidate{
			route:          route,
			matchStrength:  matchStrength,
			endpointWeight: endpointWeight,
			prefixLen:      prefixLen,
		})
	}
	if len(candidates) == 0 {
		return CompositeModelRoute{}, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.matchStrength != b.matchStrength {
			return a.matchStrength > b.matchStrength
		}
		if a.endpointWeight != b.endpointWeight {
			return a.endpointWeight > b.endpointWeight
		}
		if a.prefixLen != b.prefixLen {
			return a.prefixLen > b.prefixLen
		}
		if a.route.Priority != b.route.Priority {
			return a.route.Priority < b.route.Priority
		}
		return a.route.ID < b.route.ID
	})
	return candidates[0].route, true
}
