// Composables
import { createRouter, createWebHistory } from 'vue-router'
import Login from '@/views/Login.vue'
import Data from '@/store/modules/data'
import { liveSubscribe, type LiveSub } from '@/plugins/ws'

const routes = [
  {
    path: '/login',
    name: 'pages.login',
    component: Login,
  },
  {
    path: '/',
    component: () => import('@/layouts/default/Default.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '/',
        name: 'pages.home',
        component: () => import('@/views/Home.vue'),
      },
      {
        path: '/inbounds',
        name: 'pages.inbounds',
        component: () => import('@/views/Inbounds.vue'),
      },
      {
        path: '/clients',
        name: 'pages.clients',
        component: () => import('@/views/Clients.vue'),
      },  
      {
        path: '/outbounds',
        name: 'pages.outbounds',
        component: () => import('@/views/Outbounds.vue'),
      },
      {
        path: '/services',
        name: 'pages.services',
        component: () => import('@/views/Services.vue'),
      },
      {
        path: '/endpoints',
        name: 'pages.endpoints',
        component: () => import('@/views/Endpoints.vue'),
      },
      {
        path: '/rules',
        name: 'pages.rules',
        component: () => import('@/views/Rules.vue'),
      },
      {
        path: '/tls',
        name: 'pages.tls',
        component: () => import('@/views/Tls.vue'),
      },
      {
        path: '/basics',
        name: 'pages.basics',
        component: () => import('@/views/Basics.vue'),
      },
      {
        path: '/dns',
        name: 'pages.dns',
        component: () => import('@/views/Dns.vue'),
      },
      {
        path: '/nodes',
        name: 'pages.nodes',
        component: () => import('@/views/Nodes.vue'),
      },
      {
        path: '/admins',
        name: 'pages.admins',
        component: () => import('@/views/Admins.vue'),
      },
      {
        path: '/settings',
        name: 'pages.settings',
        component: () => import('@/views/Settings.vue'),
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory((window as any).BASE_URL),
  routes,
})

const DEFAULT_TITLE = '2S-UI'
let loadLive: LiveSub | null = null

// Chunk names carry a content hash and build.sh wipes web/html before copying, so
// after an in-place upgrade an already-open tab asks for chunks the server no longer
// has. Reload once to pick up the new index.html; the flag keeps a genuinely
// unreachable server from turning that into a reload loop.
const RELOADED_KEY = '2sui-chunk-reload'

router.onError((err, to) => {
  if (!/dynamically imported module|Importing a module script failed|Failed to fetch/i.test(String(err))) return
  if (sessionStorage.getItem(RELOADED_KEY)) return
  sessionStorage.setItem(RELOADED_KEY, '1')
  // Reload at the target, not in place. vue-router commits the URL only once the
  // navigation resolves, so location.reload() would re-open the route being left and
  // the user would have to click again; resolve().href re-attaches BASE_URL, which
  // fullPath does not carry. It also lets the flag converge — if the chunk is still
  // missing the initial navigation fails too, afterEach never runs, and the second
  // onError stops. Reloading in place landed on a working route, which cleared the
  // flag and re-armed the whole thing on the next click.
  window.location.assign(router.resolve(to.fullPath).href)
})

router.afterEach(() => sessionStorage.removeItem(RELOADED_KEY))

// Navigation guard to check authentication state
router.beforeEach((to) => {
  // Live data rides the websocket 'load' topic: a full snapshot on subscribe
  // (gated by lu so reconnects stay cheap), pushes afterwards. Stopping on the
  // login page is what closes the shared socket after logout.
  if (to.path !== '/login') {
    loadLive ??= liveSubscribe({
      topic: 'load',
      params: () => ({ lu: Data().lastLoad }),
      onData: (d) => Data().applyLive(d),
    })
  } else {
    loadLive?.stop()
    loadLive = null
  }
})

export default router
