import { defineComponent } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminGroup } from "@/types";
import GroupsView from "@/views/admin/GroupsView.vue";

const {
  listGroups,
  listCompositeRoutes,
  createCompositeRoute,
  previewCompositeRoute,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  listCompositeRoutes: vi.fn(),
  createCompositeRoute: vi.fn(),
  previewCompositeRoute: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
}));

vi.mock("@/api/admin", () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: vi.fn(),
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      listCompositeRoutes,
      createCompositeRoute,
      updateCompositeRoute: vi.fn(),
      deleteCompositeRoute: vi.fn(),
      previewCompositeRoute,
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      duplicate: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn(),
    },
  },
}));

vi.mock("@/stores/app", () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}));

vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({ isSimpleMode: false }),
}));

vi.mock("@/stores/onboarding", () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

const makeGroup = (
  overrides: Partial<AdminGroup> & Pick<AdminGroup, "id" | "name" | "platform">,
): AdminGroup =>
  ({
    id: overrides.id,
    name: overrides.name,
    description: null,
    platform: overrides.platform,
    rate_multiplier: 1,
    is_exclusive: false,
    status: "active",
    subscription_type: "standard",
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    long_context_pricing_enabled: true,
    allow_image_generation: false,
    allow_batch_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    batch_image_discount_multiplier: 0.5,
    batch_image_hold_multiplier: 0.6,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    video_price_480p: null,
    video_price_720p: null,
    video_price_1080p: null,
    web_search_price_per_call: null,
    search_price_per_1k: null,
    audio_realtime_price_per_min: null,
    audio_tts_price_per_million_chars: null,
    audio_stt_price_per_hour: null,
    peak_rate_enabled: false,
    peak_start: "",
    peak_end: "",
    peak_rate_multiplier: 1,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    allow_messages_dispatch: false,
    allow_live: false,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: "2026-09-05T00:00:00Z",
    updated_at: "2026-09-05T00:00:00Z",
    force_openai_fast: false,
    free_openai_fast: false,
    model_pricing: [],
    profit_control_enabled: false,
    profit_min_margin: 0,
    profit_safety_buffer: 0,
    model_routing: null,
    model_routing_enabled: false,
    mcp_xml_inject: false,
    supported_model_scopes: [],
    sort_order: 10,
    account_count: 1,
    active_account_count: 1,
    rate_limited_account_count: 0,
    ...overrides,
  }) as AdminGroup;

const compositeGroup = makeGroup({ id: 7, name: "Composite", platform: "composite" });
const sourceGroups = [
  makeGroup({ id: 42, name: "gpt", platform: "openai" }),
  makeGroup({ id: 23, name: "gpt", platform: "openai" }),
  makeGroup({ id: 101, name: "国内 Composite", platform: "composite" }),
  makeGroup({ id: 8, name: "inactive", platform: "openai", status: "inactive" }),
  makeGroup({ id: 9, name: "subscription", platform: "openai", subscription_type: "subscription" }),
  makeGroup({ id: 10, name: "exclusive", platform: "openai", is_exclusive: true }),
  compositeGroup,
];

const AppLayoutStub = defineComponent({ template: "<main><slot /></main>" });
const TablePageLayoutStub = defineComponent({
  template: "<section><slot name='filters' /><slot name='table' /><slot name='pagination' /></section>",
});
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template:
    "<div><div v-for='row in data' :key='row.id'><slot name='cell-actions' :row='row' /></div></div>",
});
const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: "<div v-if='show'><slot /><slot name='footer' /></div>",
});
const SelectStub = defineComponent({
  inheritAttrs: false,
  props: {
    modelValue: { type: [String, Number, Boolean], default: null },
    options: { type: Array, default: () => [] },
  },
  emits: ["update:modelValue"],
  setup(_props, { emit }) {
    const update = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value;
      emit("update:modelValue", value === "" ? null : Number(value));
    };
    return { update };
  },
  template: `
    <select v-bind="$attrs" :value="modelValue == null ? '' : modelValue"
      @change="update">
      <option v-for="option in options" :key="String(option.value)" :value="option.value == null ? '' : option.value">{{ option.label }}</option>
    </select>
  `,
});

const mountView = () =>
  mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        ReasoningEffortPolicyFields: true,
        CodexManifestAccountsField: true,
        PricingEntryCard: true,
        VueDraggable: true,
      },
    },
  });

describe("GroupsView Composite source group", () => {
  beforeEach(() => {
    localStorage.clear();
    listGroups.mockReset();
    listCompositeRoutes.mockReset();
    createCompositeRoute.mockReset();
    previewCompositeRoute.mockReset();
    getModelAllowlistCandidates.mockReset();
    getUsageSummary.mockReset();
    getCapacitySummary.mockReset();
    getLiveCapability.mockReset();

    listGroups.mockImplementation((_page: number, _pageSize: number, filters?: { status?: string }) =>
      filters?.status === "active"
        ? Promise.resolve({ items: sourceGroups, total: sourceGroups.length, page: 1, page_size: 1000, pages: 1 })
        : Promise.resolve({ items: [compositeGroup], total: 1, page: 1, page_size: 20, pages: 1 }),
    );
    listCompositeRoutes.mockResolvedValue([]);
    createCompositeRoute.mockResolvedValue({
      id: 1,
      group_id: compositeGroup.id,
      source_group_id: 42,
      public_model: "gpt-016",
      match_type: "exact",
      target_platform: "openai",
      upstream_model: "gpt-5",
      endpoint: "any",
      priority: 100,
      enabled: true,
      notes: "",
    });
    previewCompositeRoute.mockResolvedValue({
      matched: true,
      source: "route",
      group_id: compositeGroup.id,
      public_model: "gpt-023",
      target_platform: "openai",
      upstream_model: "gpt-5",
      endpoint: "any",
    });
    getModelAllowlistCandidates.mockResolvedValue([]);
    getUsageSummary.mockResolvedValue([]);
    getCapacitySummary.mockResolvedValue([]);
    getLiveCapability.mockResolvedValue({ supported: false });
  });

  it("filters eligible groups, keeps same-name IDs distinct, and forwards source IDs", async () => {
    const wrapper = mountView();
    await flushPromises();
    await wrapper.get("[data-testid='group-composite-routes']").trigger("click");
    await flushPromises();

    const sourceSelect = wrapper.get("[data-testid='composite-route-source-group']");
    const sourceOptions = sourceSelect.findAll("option").map((option) => option.text());
    expect(sourceOptions).toEqual([
      "admin.groups.compositeRoutes.sourceGroupCurrent",
      "gpt (#23)",
      "gpt (#42)",
      "国内 Composite (#101)",
    ]);
    expect(listGroups).toHaveBeenCalledWith(1, 1000, {
      status: "active",
      is_exclusive: false,
    });

    await sourceSelect.setValue("42");
    await wrapper.find("form input[placeholder='openrouter/gpt-5']").setValue("gpt-016");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(createCompositeRoute).toHaveBeenCalledWith(
      compositeGroup.id,
      expect.objectContaining({ public_model: "gpt-016", source_group_id: 42 }),
    );

    const previewSourceSelect = wrapper.get("[data-testid='composite-preview-source-group']");
    await previewSourceSelect.setValue("23");
    await wrapper
      .findAll("input[placeholder='openrouter/gpt-5']")
      .at(-1)!
      .setValue("gpt-023");
    await wrapper.get("[data-testid='composite-preview-run']").trigger("click");
    await flushPromises();
    expect(previewCompositeRoute).toHaveBeenCalledWith(
      compositeGroup.id,
      expect.objectContaining({ model: "gpt-023", source_group_id: 23 }),
    );

    wrapper.unmount();
  });
});
