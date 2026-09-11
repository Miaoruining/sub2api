package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CompositeSourceModel is the directory representation of one exact source
// route. PublicModel is the name exposed by the entry Composite group;
// UpstreamModel is the model whose source group's pricing is inherited.
// SourceGroup is the terminal eligible public standard group that owns the
// pricing context. It is kept internal to the service package and is never
// serialized directly.
type CompositeSourceModel struct {
	PublicModel   string
	UpstreamModel string
	Platform      string
	Pricing       *ChannelModelPricing
	SourceGroup   *Group
}

// CompositeSourceCatalog contains only route driven models. RouteDriven is
// deliberately separate from ModelsByGroup: an invalid/ineligible source
// route still switches a Composite group to fail-closed route semantics, so
// its old channel-wide model expansion cannot leak unrelated models.
type CompositeSourceCatalog struct {
	ModelsByGroup map[int64][]CompositeSourceModel
	RouteDriven   map[int64]bool
}

// buildCompositeSourceCatalog discovers enabled exact source routes for all
// active standard Composite groups. A source group must be active, public, and
// use the standard balance mode. A source Composite group is checked against
// its own ordinary exact/prefix routes, but source-group routes are never
// recursively followed.
//
// A nil route repository means the feature is not wired; callers retain the
// pre-existing channel based Composite behavior in that case.
func buildCompositeSourceCatalog(
	ctx context.Context,
	routeRepo CompositeModelRouteRepository,
	groups []Group,
	channels []Channel,
	pricingService *PricingService,
) (*CompositeSourceCatalog, error) {
	catalog := &CompositeSourceCatalog{
		ModelsByGroup: make(map[int64][]CompositeSourceModel),
		RouteDriven:   make(map[int64]bool),
	}
	if routeRepo == nil {
		return catalog, nil
	}

	groupByID := make(map[int64]*Group, len(groups))
	for i := range groups {
		groupByID[groups[i].ID] = &groups[i]
	}

	// Cache route reads because a source Composite may itself be referenced by
	// multiple entry groups or routes.
	routeCache := make(map[int64][]CompositeModelRoute)
	loadedRoutes := make(map[int64]bool)
	loadRoutes := func(groupID int64) ([]CompositeModelRoute, error) {
		if loadedRoutes[groupID] {
			return routeCache[groupID], nil
		}
		// Include disabled rows so a nested source-group reference is rejected in
		// the same way as request-time validation. Callers filter enabled routes.
		routes, err := routeRepo.ListByGroup(ctx, groupID, true)
		if err != nil {
			return nil, fmt.Errorf("list composite source routes for group %d: %w", groupID, err)
		}
		loadedRoutes[groupID] = true
		routeCache[groupID] = routes
		return routes, nil
	}

	for i := range groups {
		entry := &groups[i]
		if entry.Platform != PlatformComposite || entry.ID <= 0 {
			continue
		}
		routes, err := loadRoutes(entry.ID)
		if err != nil {
			return nil, err
		}
		if entry.SubscriptionType != SubscriptionTypeStandard {
			// Keep this group route-driven so an invalid configuration cannot
			// fall back to unrelated channel models.
			if hasConfiguredSourceRoute(routes) {
				catalog.RouteDriven[entry.ID] = true
			}
			continue
		}
		seen := make(map[string]struct{})
		for _, route := range routes {
			if route.SourceGroupID != nil {
				// Any configured source route makes the group route-driven. Exact
				// routes below are the enumerable public model catalog.
				catalog.RouteDriven[entry.ID] = true
			}
			if !isEnabledExactSourceRoute(route) {
				continue
			}
			resolved := resolveCompositeSourceTarget(route, entry.ID, groupByID, loadRoutes)
			if resolved == nil {
				continue
			}
			publicModel := strings.TrimSpace(route.PublicModel)
			if publicModel == "" || resolved.Platform == "" {
				continue
			}
			key := strings.ToLower(resolved.Platform) + "\x00" + strings.ToLower(publicModel)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			pricing := sourceRoutePricing(resolved.SourceGroup, resolved.UpstreamModel, resolved.Platform, channels, pricingService)
			catalog.ModelsByGroup[entry.ID] = append(catalog.ModelsByGroup[entry.ID], CompositeSourceModel{
				PublicModel:   publicModel,
				UpstreamModel: resolved.UpstreamModel,
				Platform:      resolved.Platform,
				Pricing:       pricing,
				SourceGroup:   resolved.SourceGroup,
			})
		}
	}

	for groupID := range catalog.ModelsByGroup {
		sort.SliceStable(catalog.ModelsByGroup[groupID], func(i, j int) bool {
			a, b := catalog.ModelsByGroup[groupID][i], catalog.ModelsByGroup[groupID][j]
			if a.PublicModel != b.PublicModel {
				return a.PublicModel < b.PublicModel
			}
			return a.Platform < b.Platform
		})
	}
	return catalog, nil
}

type compositeSourceTarget struct {
	SourceGroup   *Group
	UpstreamModel string
	Platform      string
}

func resolveCompositeSourceTarget(
	route CompositeModelRoute,
	entryGroupID int64,
	groups map[int64]*Group,
	loadRoutes func(int64) ([]CompositeModelRoute, error),
) *compositeSourceTarget {
	if route.SourceGroupID == nil || strings.TrimSpace(route.PublicModel) == "" {
		return nil
	}
	sourceID := *route.SourceGroupID
	if sourceID <= 0 || sourceID == entryGroupID {
		return nil
	}
	source := groups[sourceID]
	if !eligibleCompositeSourceGroup(source) {
		return nil
	}
	model := strings.TrimSpace(route.UpstreamModel)
	if model == "" {
		model = strings.TrimSpace(route.PublicModel)
	}
	platform := strings.TrimSpace(route.TargetPlatform)
	if !isConcreteRequestPlatform(platform) {
		return nil
	}
	if source.ModelAllowlistEnabled() && !source.ModelAllowlist.Allows(model) {
		return nil
	}
	if source.Platform != PlatformComposite && source.Platform != platform {
		return nil
	}

	// A Composite source can have its own ordinary exact/prefix route to
	// establish the concrete provider. Runtime validation rejects any nested
	// source-group route, including disabled rows, so reject the whole source
	// before considering ordinary routes. The nested route is only a provider
	// check; its upstream model is not recursively substituted at runtime.
	if source.Platform == PlatformComposite {
		nestedRoutes, err := loadRoutes(source.ID)
		if err != nil {
			return nil
		}
		plainRoutes := make([]CompositeModelRoute, 0, len(nestedRoutes))
		for _, nested := range nestedRoutes {
			if nested.SourceGroupID != nil {
				return nil
			}
			if !nested.Enabled {
				continue
			}
			plainRoutes = append(plainRoutes, nested)
		}
		if detected, ok := DetectModelPlatform(model); ok {
			if detected != platform {
				return nil
			}
			return &compositeSourceTarget{SourceGroup: source, UpstreamModel: model, Platform: platform}
		}
		nested, ok := matchCompositeRoute(plainRoutes, model, normalizeCompositeRouteEndpoint(route.Endpoint))
		if !ok || !isConcreteRequestPlatform(strings.TrimSpace(nested.TargetPlatform)) || strings.TrimSpace(nested.TargetPlatform) != platform {
			return nil
		}
	}
	return &compositeSourceTarget{SourceGroup: source, UpstreamModel: model, Platform: platform}
}

func isEnabledExactSourceRoute(route CompositeModelRoute) bool {
	return route.Enabled && normalizeCompositeRouteMatchType(route.MatchType) == CompositeRouteMatchExact &&
		route.SourceGroupID != nil && strings.TrimSpace(route.PublicModel) != ""
}

func hasConfiguredSourceRoute(routes []CompositeModelRoute) bool {
	for _, route := range routes {
		if route.SourceGroupID != nil {
			return true
		}
	}
	return false
}

func eligibleCompositeSourceGroup(group *Group) bool {
	return group != nil && group.ID > 0 && group.Status == StatusActive &&
		!group.IsExclusive && group.SubscriptionType == SubscriptionTypeStandard
}

func sourceRoutePricing(source *Group, model, platform string, channels []Channel, pricingService *PricingService) *ChannelModelPricing {
	if source == nil {
		return nil
	}
	// Group cards have precedence in the normal pricing resolver. Returning a
	// clone here also makes the no-billing-service test path inherit the card.
	if pricing := matchGroupModelPricing(source, model); pricing != nil {
		return pricing
	}
	for i := range channels {
		ch := &channels[i]
		if ch.Status != StatusActive || !channelHasGroup(ch, source.ID) {
			continue
		}
		if pricing := ch.GetModelPricing(model); pricing != nil {
			cloned := pricing.Clone()
			return &cloned
		}
		supported := ch.SupportedModels()
		fillGlobalPricingFallback(pricingService, supported)
		for j := range supported {
			if strings.EqualFold(supported[j].Platform, platform) && strings.EqualFold(strings.TrimSpace(supported[j].Name), strings.TrimSpace(model)) {
				return supported[j].Pricing
			}
		}
	}
	return nil
}

func channelHasGroup(ch *Channel, groupID int64) bool {
	if ch == nil {
		return false
	}
	for _, id := range ch.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}
