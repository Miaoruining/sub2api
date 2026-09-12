import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  route: {
    path: '/home',
    fullPath: '/home',
    meta: {},
  },
  router: {
    afterEach: vi.fn(),
    replace: vi.fn(),
  },
  appStore: {
    cachedPublicSettings: null,
    siteName: 'Sub2API',
    siteLogo: '',
    publicSettingsLoaded: false,
    fetchPublicSettings: vi.fn(),
  },
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
  },
  subscriptionStore: {
    fetchActiveSubscriptions: vi.fn(),
    startPolling: vi.fn(),
    clear: vi.fn(),
  },
  announcementStore: {
    fetchAnnouncements: vi.fn(),
    reset: vi.fn(),
  },
  adminComplianceStore: {
    fetchStatus: vi.fn(),
    reset: vi.fn(),
    requireAcknowledgement: vi.fn(),
  },
  adminSettingsStore: {
    customMenuItems: [],
  },
  getSetupStatus: vi.fn(),
  updateFavicon: vi.fn(),
}))

vi.mock('vue-router', () => ({
  RouterView: { template: '<div />' },
  useRoute: () => mocks.route,
  useRouter: () => mocks.router,
}))

vi.mock('@/api/setup', () => ({
  getSetupStatus: mocks.getSetupStatus,
}))

vi.mock('@/stores', () => ({
  useAppStore: () => mocks.appStore,
  useAuthStore: () => mocks.authStore,
  useSubscriptionStore: () => mocks.subscriptionStore,
  useAnnouncementStore: () => mocks.announcementStore,
  useAdminComplianceStore: () => mocks.adminComplianceStore,
  useAdminSettingsStore: () => mocks.adminSettingsStore,
}))

vi.mock('@/router/title', () => ({
  resolveRouteDocumentTitle: () => 'Test title',
}))

vi.mock('@/utils/branding', () => ({
  updateFavicon: mocks.updateFavicon,
}))

vi.mock('@/components/common/Toast.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/NavigationProgress.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/admin/AdminComplianceDialog.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/common/AnnouncementPopup.vue', () => ({ default: { template: '<div />' } }))

import App from '@/App.vue'

describe('App setup probe', () => {
  let wrapper: VueWrapper

  beforeEach(() => {
    mocks.route.path = '/home'
    mocks.route.fullPath = '/home'
    mocks.route.meta = {}
    mocks.router.afterEach.mockReset()
    mocks.router.replace.mockReset()
    mocks.appStore.cachedPublicSettings = null
    mocks.appStore.publicSettingsLoaded = false
    mocks.appStore.fetchPublicSettings.mockReset()
    mocks.authStore.isAuthenticated = false
    mocks.authStore.isAdmin = false
    mocks.subscriptionStore.fetchActiveSubscriptions.mockReset()
    mocks.subscriptionStore.startPolling.mockReset()
    mocks.subscriptionStore.clear.mockReset()
    mocks.announcementStore.fetchAnnouncements.mockReset()
    mocks.announcementStore.reset.mockReset()
    mocks.adminComplianceStore.fetchStatus.mockReset()
    mocks.adminComplianceStore.reset.mockReset()
    mocks.adminComplianceStore.requireAcknowledgement.mockReset()
    mocks.getSetupStatus.mockReset()
    mocks.getSetupStatus.mockResolvedValue({ needs_setup: false, step: 'completed' })
    mocks.updateFavicon.mockReset()
    delete window.__APP_CONFIG__
  })

  afterEach(() => {
    wrapper?.unmount()
    delete window.__APP_CONFIG__
  })

  it('has server injected settings and skips the redundant setup probe', async () => {
    window.__APP_CONFIG__ = {
      site_name: 'ModelPort',
    }

    wrapper = mount(App)
    await flushPromises()

    expect(mocks.getSetupStatus).not.toHaveBeenCalled()
    expect(mocks.appStore.fetchPublicSettings).toHaveBeenCalledOnce()
  })

  it('without injected settings keeps the setup probe before loading settings', async () => {
    wrapper = mount(App)
    await flushPromises()

    expect(mocks.getSetupStatus).toHaveBeenCalledOnce()
    expect(mocks.appStore.fetchPublicSettings).toHaveBeenCalledOnce()
    expect(mocks.router.replace).not.toHaveBeenCalled()
  })

  it('redirects to setup and does not load settings when installation is incomplete', async () => {
    mocks.getSetupStatus.mockResolvedValue({ needs_setup: true, step: 'welcome' })

    wrapper = mount(App)
    await flushPromises()

    expect(mocks.router.replace).toHaveBeenCalledWith('/setup')
    expect(mocks.appStore.fetchPublicSettings).not.toHaveBeenCalled()
  })
})
