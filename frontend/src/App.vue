<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  Bootstrap,
  CopyText,
  CopySecret,
  CreateProfile,
  DeleteShortcut,
  HideWindow,
  InsertText,
  LockProfile,
  OpenCompact,
  OpenManager,
  Quit,
  RenderEntry,
  RenderShortcut,
  SaveShortcut,
  SwitchProfile,
  UnlockProfile,
  UseShortcut,
} from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'

interface Profile {
  id: string
  name: string
  createdAt: string
  lastUsedAt: string
}

interface VaultStatus {
  exists: boolean
  unlocked: boolean
  autoLockSeconds: number
  remainingSeconds: number
  storedSecretCount: number
  storedShortcutCount: number
}

interface Shortcut {
  id: string
  trigger: string
  title: string
  category?: string
  kind?: string
  template?: string
  variables?: Variable[]
  fields?: Record<string, string>
  content: string
  secretId?: string
  sensitive?: boolean
  updatedAt: string
}

interface Variable {
  name: string
  label: string
  type: string
  default?: string
  placeholder?: string
  required?: boolean
  options?: string[]
}

interface Entry {
  id: string
  trigger: string
  title: string
  description?: string
  category: string
  template: string
  variables?: Variable[]
  favorite?: boolean
  riskLevel?: string
}

interface QuickResult {
  id: string
  trigger: string
  title: string
  category: string
  secretId?: string
  source: 'shortcut' | 'builtin'
  shortcut?: Shortcut
  entry?: Entry
}

interface BootstrapState {
  entries: Entry[]
  profiles: Profile[]
  activeProfile?: Profile
  vault: VaultStatus
  shortcuts: Shortcut[]
  hotkeyAvailable: boolean
  hotkeyMessage: string
  profileRoot: string
  vaultFile?: string
  logFile: string
  initError?: string
}

const emptyVault: VaultStatus = {
  exists: false,
  unlocked: false,
  autoLockSeconds: 86400,
  remainingSeconds: 0,
  storedSecretCount: 0,
  storedShortcutCount: 0,
}

const state = reactive<BootstrapState>({
  entries: [],
  profiles: [],
  vault: { ...emptyVault },
  shortcuts: [],
  hotkeyAvailable: false,
  hotkeyMessage: '',
  profileRoot: '',
  logFile: '',
})

const loading = ref(true)
const surface = ref<'quick' | 'manager'>('quick')
const query = ref('')
const searchInput = ref<HTMLInputElement | null>(null)
const errorMessage = ref('')
const toast = ref('')
const pendingTrigger = ref('')
const editorOpen = ref(false)
const activeResultIndex = ref(0)
const creatingProfile = ref(false)
const selectedProfileID = ref('')
const profileName = ref('')
const password = ref('')
const passwordConfirm = ref('')
const secretPassword = ref('')
let toastTimer: number | undefined
const eventStops: Array<() => void> = []

const editor = reactive<Shortcut>({
  id: '',
  trigger: ':',
  title: '',
  category: 'common',
  template: '',
  variables: [],
  fields: {},
  content: '',
  sensitive: false,
  updatedAt: '',
})

const authenticated = computed(() => Boolean(state.activeProfile && state.vault.unlocked))
const isCreatingProfile = computed(() => creatingProfile.value || state.profiles.length === 0)
const filteredShortcuts = computed(() => {
  const tokens = query.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return state.shortcuts.filter((item) => {
    const text = [item.trigger, item.title, item.category, item.content].join(' ').toLowerCase()
    return tokens.every((token) => text.includes(token))
  }).slice(0, 100)
})
const groups = computed(() => {
  const result: Record<string, Shortcut[]> = {}
  for (const item of state.shortcuts) {
    const category = item.category || 'common'
    result[category] ||= []
    result[category].push(item)
  }
  return result
})
const quickResults = computed<QuickResult[]>(() => {
  const tokens = query.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
  const matches = (values: Array<string | undefined>) => {
    const text = values.join(' ').toLowerCase()
    return tokens.every((token) => text.includes(token))
  }
  const userItems = state.shortcuts
    .filter((item) => matches([item.trigger, item.title, item.category, item.content]))
    .map((item): QuickResult => ({
      id: item.id,
      trigger: item.trigger,
      title: item.title,
      category: item.category || 'common',
      secretId: item.secretId,
      source: 'shortcut',
      shortcut: item,
    }))
  const userTriggers = new Set(userItems.map((item) => item.trigger))
  const builtInItems = state.entries
    .filter((entry) => !userTriggers.has(entry.trigger))
    .filter((entry) => tokens.length ? matches([entry.trigger, entry.title, entry.description, entry.category]) : entry.favorite)
    .map((entry): QuickResult => ({
      id: entry.id,
      trigger: entry.trigger,
      title: entry.title,
      category: entry.category,
      source: 'builtin',
      entry,
    }))
  return [...userItems, ...builtInItems].slice(0, 8)
})
const builtInGroups = computed(() => {
  const order = ['mysql', 'redis', 'kafka', 'docker', 'linux', 'nginx', 'git', 'common', 'ai']
  const grouped = new Map<string, Entry[]>()
  for (const entry of state.entries) {
    if (!grouped.has(entry.category)) grouped.set(entry.category, [])
    grouped.get(entry.category)!.push(entry)
  }
  return [...grouped.entries()]
    .sort(([left], [right]) => {
      const leftIndex = order.indexOf(left)
      const rightIndex = order.indexOf(right)
      return (leftIndex < 0 ? 999 : leftIndex) - (rightIndex < 0 ? 999 : rightIndex) || left.localeCompare(right)
    })
    .map(([category, entries]) => ({ category, entries }))
})
const categoryOptions = computed(() => {
  const categories = new Set<string>(['common'])
  for (const entry of state.entries) if (entry.category) categories.add(entry.category)
  for (const item of state.shortcuts) if (item.category) categories.add(item.category)
  return [...categories].sort((left, right) => left.localeCompare(right))
})
const editorVariables = computed(() => editor.variables ?? [])

watch(query, () => {
  activeResultIndex.value = 0
})
watch(() => editor.template, () => {
  if (editorOpen.value) syncEditorVariables()
})

function explainError(error: unknown) {
  return (error instanceof Error ? error.message : String(error)).replace(/^Error:\s*/, '')
}

function message(text: string) {
  toast.value = text
  if (toastTimer) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => { toast.value = '' }, 2600)
}

function applyState(next: BootstrapState) {
  Object.assign(state, {
    ...next,
    entries: next.entries ?? [],
    profiles: next.profiles ?? [],
    shortcuts: next.shortcuts ?? [],
    vault: next.vault ?? { ...emptyVault },
  })
}

async function load() {
  loading.value = true
  try {
    applyState(await Bootstrap() as BootstrapState)
    selectedProfileID.value = state.activeProfile?.id || state.profiles[0]?.id || ''
  } catch (error) {
    errorMessage.value = explainError(error)
  } finally {
    loading.value = false
  }
}

async function submitProfile() {
  errorMessage.value = ''
  if (!password.value) return
  if (isCreatingProfile.value && password.value.length < 8) {
    errorMessage.value = '主密码至少需要 8 个字符'
    return
  }
  if (isCreatingProfile.value && password.value !== passwordConfirm.value) {
    errorMessage.value = '两次输入的主密码不一致'
    return
  }
  try {
    const next = isCreatingProfile.value
      ? await CreateProfile(profileName.value, password.value)
      : await UnlockProfile(selectedProfileID.value, password.value)
    applyState(next as BootstrapState)
    selectedProfileID.value = state.activeProfile?.id || ''
    password.value = ''
    passwordConfirm.value = ''
    profileName.value = ''
    creatingProfile.value = false
    message('本地用户已解锁')
    if (pendingTrigger.value) openNewShortcut(pendingTrigger.value)
    else nextTick(() => searchInput.value?.focus())
  } catch (error) {
    password.value = ''
    passwordConfirm.value = ''
    errorMessage.value = explainError(error)
  }
}

async function lock() {
  applyState(await LockProfile() as BootstrapState)
  password.value = ''
  query.value = ''
}

async function switchUser() {
  applyState(await SwitchProfile() as BootstrapState)
  selectedProfileID.value = state.profiles[0]?.id || ''
  surface.value = 'quick'
}

function resetEditor(trigger = ':') {
  Object.assign(editor, {
    id: '',
    trigger,
    title: trigger.length > 1 ? trigger.slice(1).replace(/[-_]/g, ' ') : '',
    category: trigger.includes('mysql') ? 'mysql' : 'common',
    kind: '',
    template: '',
    variables: [],
    fields: {},
    content: '',
    secretId: '',
    sensitive: false,
    updatedAt: '',
  })
  secretPassword.value = ''
}

function variablesInTemplate(template: string, declared: Variable[] = []) {
  const metadata = new Map(declared.map((variable) => [variable.name, variable]))
  const seen = new Set<string>()
  const result: Variable[] = []
  const pattern = /\{\{\s*([a-zA-Z][a-zA-Z0-9_-]*)\s*\}\}/g
  for (const match of template.matchAll(pattern)) {
    const name = match[1]
    if (seen.has(name)) continue
    seen.add(name)
    const existing = metadata.get(name)
    result.push(existing
      ? { ...existing, label: existing.label || name, type: existing.type || 'text' }
      : { name, label: name, type: 'text', default: name })
  }
  return result
}

function syncEditorVariables() {
  const variables = variablesInTemplate(editor.template || '', editor.variables || [])
  const current = editor.fields || {}
  const fields: Record<string, string> = {}
  for (const variable of variables) {
    fields[variable.name] = current[variable.name] ?? variable.default ?? ''
  }
  editor.variables = variables
  editor.fields = fields
}

function openNewShortcut(trigger = ':') {
  pendingTrigger.value = trigger
  resetEditor(trigger)
  editorOpen.value = true
  nextTick(() => {
    const element = document.querySelector<HTMLInputElement>('[data-editor-first]')
    element?.focus()
    element?.select()
  })
}

function editShortcut(item: Shortcut) {
  const editable = JSON.parse(JSON.stringify(item)) as Shortcut
  if (editable.kind === 'mysql' && !editable.template) {
    const legacyFields = editable.fields || {}
    editable.template = 'mysql --host={{MYSQL_HOST}} --port={{MYSQL_PORT}} --user={{MYSQL_USER}} -p {{MYSQL_DATABASE}}'
    editable.variables = [
      { name: 'MYSQL_HOST', label: '主机', type: 'text', default: 'MYSQL_HOST' },
      { name: 'MYSQL_PORT', label: '端口', type: 'text', default: '3306' },
      { name: 'MYSQL_USER', label: '用户', type: 'text', default: 'MYSQL_USER' },
      { name: 'MYSQL_DATABASE', label: '数据库', type: 'text', default: 'MYSQL_DATABASE' },
    ]
    editable.fields = {
      MYSQL_HOST: legacyFields.host || 'MYSQL_HOST',
      MYSQL_PORT: legacyFields.port || '3306',
      MYSQL_USER: legacyFields.username || 'MYSQL_USER',
      MYSQL_DATABASE: legacyFields.database || 'MYSQL_DATABASE',
    }
  } else {
    editable.template ||= editable.content
  }
  editable.variables ||= []
  editable.fields ||= {}
  editable.kind = ''
  Object.assign(editor, editable)
  syncEditorVariables()
  secretPassword.value = ''
  editorOpen.value = true
}

async function saveEditor() {
  try {
    syncEditorVariables()
    const saved = await SaveShortcut(JSON.parse(JSON.stringify(editor)), secretPassword.value) as Shortcut
    const index = state.shortcuts.findIndex((item) => item.id === saved.id)
    if (index >= 0) state.shortcuts[index] = saved
    else state.shortcuts.push(saved)
    state.vault.storedShortcutCount = state.shortcuts.length
    editorOpen.value = false
    pendingTrigger.value = ''
    secretPassword.value = ''
    query.value = saved.trigger
    message('已加密保存，以后只需触发词')
    if (surface.value === 'quick') nextTick(() => searchInput.value?.focus())
  } catch (error) {
    secretPassword.value = ''
    errorMessage.value = explainError(error)
  }
}

async function removeShortcut(item: Shortcut) {
  if (!window.confirm(`删除 ${item.trigger}？该操作无法撤销。`)) return
  try {
    await DeleteShortcut(item.id)
    state.shortcuts = state.shortcuts.filter((entry) => entry.id !== item.id)
    state.vault.storedShortcutCount = state.shortcuts.length
    editorOpen.value = false
    message('快捷词已删除')
  } catch (error) {
    errorMessage.value = explainError(error)
  }
}

function insertionMessage(mode: string) {
  if (mode === 'copied') message('没有可用的外部窗口，内容已复制')
  else message('已填写到上一个窗口')
}

async function use(item: Shortcut) {
  try {
    insertionMessage(await UseShortcut(item.id))
    query.value = ''
  } catch (error) {
    errorMessage.value = explainError(error)
    await load()
  }
}

async function copyLinkedSecret(item: Shortcut) {
  if (!item.secretId) return
  try {
    await CopySecret(item.secretId)
    message('密码已复制，20 秒后自动清除')
  } catch (error) {
    errorMessage.value = explainError(error)
  }
}

async function copyLogPath() {
  if (!state.logFile) return
  await CopyText(state.logFile)
  message('日志路径已复制')
}

async function useBuiltIn(entry: Entry) {
  let text: string
  try {
    text = await RenderEntry(entry.id, {})
  } catch (error) {
    configureBuiltIn(entry)
    message('请先配置模板变量，再保存为个人快捷词')
    return
  }
  try {
    insertionMessage(await InsertText(text))
  } catch (error) {
    errorMessage.value = explainError(error)
  }
}

async function copyQuickResult(item: QuickResult) {
  try {
    const text = item.source === 'shortcut' && item.shortcut
      ? await RenderShortcut(item.shortcut.id)
      : item.entry
        ? await RenderEntry(item.entry.id, {})
        : ''
    if (!text) return
    await CopyText(text)
    message(`已复制 ${item.trigger}`)
  } catch (error) {
    errorMessage.value = explainError(error)
  }
}

function useQuickResult(item: QuickResult) {
  if (item.source === 'shortcut' && item.shortcut) {
    use(item.shortcut)
  } else if (item.entry) {
    useBuiltIn(item.entry)
  }
}

async function copyShortcut(item: Shortcut) {
  try {
    await CopyText(await RenderShortcut(item.id))
    message(`已复制 ${item.trigger}`)
  } catch (error) {
    errorMessage.value = explainError(error)
  }
}

async function copyBuiltIn(entry: Entry) {
  try {
    await CopyText(await RenderEntry(entry.id, {}))
    message(`已复制 ${entry.trigger}`)
  } catch (error) {
    errorMessage.value = explainError(error)
  }
}

function configureBuiltIn(entry: Entry) {
  const existing = state.shortcuts.find((item) => item.trigger === entry.trigger)
  if (existing) {
    editShortcut(existing)
    message('正在编辑同名的个人快捷词')
    return
  }
  resetEditor(entry.trigger)
  Object.assign(editor, {
    title: entry.title,
    category: entry.category,
    template: entry.template,
    variables: JSON.parse(JSON.stringify(entry.variables ?? [])),
    fields: Object.fromEntries((entry.variables ?? []).map((variable) => [
      variable.name,
      variable.default ?? '',
    ])),
  })
  syncEditorVariables()
  editorOpen.value = true
  nextTick(() => {
    const element = document.querySelector<HTMLInputElement>('[data-variable-first]')
    element?.focus()
    element?.select()
  })
}

function handleSearchEnter() {
  const exact = quickResults.value.find((item) => item.trigger === query.value.trim().toLowerCase())
  if (exact) {
    useQuickResult(exact)
    return
  }
  const active = quickResults.value[activeResultIndex.value]
  if (active) {
    useQuickResult(active)
    return
  }
  if (/^:[a-z0-9][a-z0-9_-]*$/.test(query.value.trim())) {
    openNewShortcut(query.value.trim().toLowerCase())
  }
}

function handleSearchKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    activeResultIndex.value = Math.min(activeResultIndex.value + 1, Math.max(quickResults.value.length - 1, 0))
    return
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    activeResultIndex.value = Math.max(activeResultIndex.value - 1, 0)
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    handleSearchEnter()
  }
}

async function showManager() {
  surface.value = 'manager'
  await OpenManager()
}

async function showCompact() {
  surface.value = 'quick'
  editorOpen.value = false
  await OpenCompact()
  nextTick(() => searchInput.value?.focus())
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    if (editorOpen.value) editorOpen.value = false
    else HideWindow()
  }
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'n' && authenticated.value) {
    event.preventDefault()
    openNewShortcut(query.value.startsWith(':') ? query.value : ':')
  }
}

onMounted(async () => {
  await load()
  window.addEventListener('keydown', handleKeydown)
  eventStops.push(EventsOn('expand:quick-action', async (payload: { mode?: string; trigger?: string }) => {
    await load()
    surface.value = 'quick'
    query.value = payload.trigger || ''
    pendingTrigger.value = payload.trigger || ''
    if (payload.mode === 'create' && authenticated.value) openNewShortcut(payload.trigger || ':')
    nextTick(() => searchInput.value?.focus())
  }))
  eventStops.push(EventsOn('expand:open-manager', () => { surface.value = 'manager' }))
  eventStops.push(EventsOn('expand:shortcuts-changed', load))
  nextTick(() => searchInput.value?.focus())
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleKeydown)
  eventStops.forEach((stop) => stop())
  if (toastTimer) window.clearTimeout(toastTimer)
})
</script>

<template>
  <main class="app" :class="[`surface-${surface}`, { loading }]">
    <header class="chrome">
      <div class="brand"><span class="brand-symbol"><i></i><i></i></span><strong>expand</strong></div>
      <div v-if="state.activeProfile" class="session">
        <i :class="{ unlocked: state.vault.unlocked }"></i>
        <span>{{ state.activeProfile.name }}</span>
      </div>
      <div class="chrome-actions">
        <button v-if="surface === 'quick' && authenticated" @click="showManager">管理</button>
        <button v-if="surface === 'manager'" @click="showCompact">收起</button>
        <button aria-label="隐藏窗口" title="隐藏到后台，保留全局快捷键" @click="HideWindow">—</button>
        <button aria-label="退出 expand" title="彻底退出 expand" @click="Quit">×</button>
      </div>
    </header>

    <section v-if="!authenticated" class="auth">
      <div class="auth-mark"><span>:</span><i></i></div>
      <div>
        <small>LOCAL ENCRYPTED PROFILE</small>
        <h1>{{ isCreatingProfile ? '创建本地用户' : '解锁 expand' }}</h1>
        <p>每个用户拥有完全独立的加密快捷库。</p>
      </div>

      <form @submit.prevent="submitProfile">
        <template v-if="isCreatingProfile">
          <label>用户名<input v-model="profileName" type="text" autocomplete="username" autofocus placeholder="例如 florune" /></label>
        </template>
        <label v-else>
          本地用户
          <select v-model="selectedProfileID">
            <option v-for="item in state.profiles" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </label>
        <label>
          主密码
          <input v-model="password" type="password" :autocomplete="isCreatingProfile ? 'new-password' : 'current-password'" />
          <small v-if="isCreatingProfile">至少 8 个字符；忘记后无法恢复</small>
        </label>
        <label v-if="isCreatingProfile">确认主密码<input v-model="passwordConfirm" type="password" autocomplete="new-password" /></label>
        <p v-if="errorMessage" class="error">{{ errorMessage }}</p>
        <button class="primary" :disabled="!password || (isCreatingProfile && !profileName)">
          {{ isCreatingProfile ? '创建并进入' : '解锁' }}
        </button>
      </form>

      <button v-if="state.profiles.length" class="link" @click="creatingProfile = !creatingProfile; errorMessage = ''">
        {{ creatingProfile ? '返回已有用户' : '创建另一个本地用户' }}
      </button>
      <footer :title="state.logFile">没有服务器 · 忘记主密码无法恢复 · 日志已启用</footer>
    </section>

    <template v-else-if="surface === 'quick'">
      <section class="quick">
        <label class="search">
          <svg aria-hidden="true" viewBox="0 0 24 24"><circle cx="11" cy="11" r="6.5"></circle><path d="m16 16 4 4"></path></svg>
          <input ref="searchInput" v-model="query" type="text" autocomplete="off" spellcheck="false"
            placeholder="输入 :trigger 或搜索…" @keydown="handleSearchKeydown" />
          <kbd>Enter</kbd>
        </label>

        <div class="quick-hint">
          <span><kbd>{{ state.hotkeyMessage || 'Ctrl + Alt + J' }}</kbd> 唤起 · 单击复制 · 双击填写</span>
          <button @click="openNewShortcut(query.startsWith(':') ? query : ':')">＋ 新建</button>
        </div>

        <div v-if="errorMessage" class="error-strip">
          <span>{{ errorMessage }}</span>
          <button v-if="state.logFile" @click="copyLogPath">复制日志路径</button>
          <button @click="errorMessage = ''">×</button>
        </div>

        <section class="results">
          <button v-for="(item, index) in quickResults" :key="`${item.source}-${item.id}`" class="result"
            :class="{ active: activeResultIndex === index }" :aria-selected="activeResultIndex === index"
            @mouseenter="activeResultIndex = index" @click="copyQuickResult(item)" @dblclick="useQuickResult(item)">
            <code>{{ item.trigger }}</code>
            <span><strong>{{ item.title }}</strong><small>{{ item.category }} · {{ item.source === 'builtin' ? '内置模板' : '个人快捷词' }}</small></span>
            <em v-if="item.secretId && item.shortcut" @click.stop="copyLinkedSecret(item.shortcut)">密码</em>
            <em v-else-if="item.entry" class="config-action" @click.stop="configureBuiltIn(item.entry)">配置</em>
            <i><small>单击</small><b>复制</b><small>双击</small><b>填写</b></i>
          </button>
          <button v-if="!quickResults.length && query.startsWith(':')" class="empty-result" @click="openNewShortcut(query)">
            <span>＋</span><strong>创建 {{ query }}</strong><small>保存一次，以后直接替换</small>
          </button>
          <div v-else-if="!quickResults.length" class="empty">
            <span>:_</span>
            <p>输入快捷词，或在其他应用选中触发词后按全局快捷键。</p>
          </div>
        </section>

        <footer class="quick-footer">
          <span>{{ state.shortcuts.length }} 个个人 · {{ state.entries.length }} 个内置</span>
          <button @click="lock">锁定</button>
          <button @click="switchUser">切换用户</button>
        </footer>
      </section>
    </template>

    <template v-else>
      <section class="manager">
        <aside>
          <div>
            <small>ENCRYPTED LIBRARY</small>
            <h1>快捷实例</h1>
            <p>编辑一次，日常只输入触发词。</p>
          </div>
          <button class="primary" @click="openNewShortcut()">＋ 新建快捷词</button>
          <nav>
            <span v-if="Object.keys(groups).length" class="nav-label">我的分类</span>
            <a v-for="(items, category) in groups" :key="category" href="#my-shortcuts">
              <span>{{ category }}</span><b>{{ items.length }}</b>
            </a>
            <span class="nav-label">内置分类</span>
            <a v-for="group in builtInGroups" :key="`builtin-${group.category}`" :href="`#builtin-${group.category}`">
              <span>{{ group.category }}</span><b>{{ group.entries.length }}</b>
            </a>
          </nav>
          <div class="manager-session">
            <strong>{{ state.activeProfile?.name }}</strong>
            <small>{{ state.vault.storedShortcutCount }} shortcuts · {{ state.vault.storedSecretCount }} secrets</small>
            <button @click="lock">立即锁定</button>
            <button @click="switchUser">切换用户</button>
            <button :title="state.logFile" @click="copyLogPath">复制日志路径</button>
            <button @click="Quit">退出 expand</button>
          </div>
        </aside>

        <div class="manager-content">
          <header>
            <div><small>MANAGE</small><h2>我的快捷词</h2></div>
            <label class="manager-search">⌕<input v-model="query" placeholder="筛选…" /></label>
          </header>

          <div v-if="errorMessage" class="error-strip"><span>{{ errorMessage }}</span><button @click="errorMessage = ''">×</button></div>

          <section id="my-shortcuts" class="shortcut-grid">
            <article v-for="item in filteredShortcuts" :key="item.id"
              @click="copyShortcut(item)" @dblclick="use(item)">
              <div><code>{{ item.trigger }}</code><span>{{ item.category || 'common' }}</span></div>
              <h3>{{ item.title }}</h3>
              <pre>{{ item.content }}</pre>
              <footer>
                <button @click.stop="copyShortcut(item)">复制</button>
                <button @click.stop="use(item)">填写</button>
                <button v-if="item.secretId" @click.stop="copyLinkedSecret(item)">复制密码 20s</button>
                <button @click.stop="editShortcut(item)">编辑</button>
              </footer>
            </article>
          </section>

          <section class="builtins">
            <header><div><small>READY TO EXPAND</small><h2>内置模板</h2></div><span>{{ state.entries.length }} templates</span></header>
            <div class="builtin-groups">
              <section v-for="group in builtInGroups" :id="`builtin-${group.category}`" :key="group.category" class="builtin-group">
                <header><h3>{{ group.category }}</h3><span>{{ group.entries.length }}</span></header>
                <div class="builtin-grid">
                  <article v-for="entry in group.entries" :key="entry.id" class="builtin-card"
                    @click="copyBuiltIn(entry)" @dblclick="useBuiltIn(entry)">
                    <code>{{ entry.trigger }}</code><span>{{ entry.title }}</span>
                    <small>{{ entry.variables?.length ? '含默认占位符' : '直接展开' }} · 单击复制 / 双击填写</small>
                    <button type="button" @click.stop="configureBuiltIn(entry)" @dblclick.stop>配置为我的快捷词</button>
                  </article>
                </div>
              </section>
            </div>
          </section>
        </div>
      </section>
    </template>

    <div v-if="editorOpen && authenticated" class="editor-layer" @mousedown.self="editorOpen = false">
      <form class="editor" @submit.prevent="saveEditor">
        <header>
          <div><small>ENCRYPTED SHORTCUT</small><h2>{{ editor.id ? '编辑快捷词' : '创建快捷词' }}</h2></div>
          <button type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="editor-grid">
          <label>触发词<input v-model="editor.trigger" data-editor-first type="text" spellcheck="false" placeholder=":mysql-connect-prod" /></label>
          <label>名称<input v-model="editor.title" type="text" placeholder="MySQL 生产连接" /></label>
          <label class="span-2">分类
            <input v-model="editor.category" list="shortcut-category-options" type="text" placeholder="选择已有分类，或输入新分类" />
            <datalist id="shortcut-category-options">
              <option v-for="category in categoryOptions" :key="category" :value="category"></option>
            </datalist>
            <small class="field-hint">已有分类可直接选择；输入一个新名称即可创建分类。</small>
          </label>
        </div>

        <label class="content-field">
          模板内容
          <textarea v-model="editor.template" rows="7" spellcheck="false"
            placeholder="例如：mysql -h {{MYSQL_HOST}} -u {{MYSQL_USER}} -p"></textarea>
          <small class="field-hint">写入 <code v-pre>{{MYSQL_USER}}</code> 这类变量后，下方会自动生成配置项；没有变量时就是普通文本。</small>
        </label>

        <section v-if="editorVariables.length" class="template-variables">
          <header>
            <div><small>VARIABLES</small><strong>模板变量</strong></div>
            <span>{{ editorVariables.length }} 项</span>
          </header>
          <div class="variable-grid">
            <label v-for="(variable, index) in editorVariables" :key="variable.name">
              <span>{{ variable.label || variable.name }} <code>{{ variable.name }}</code></span>
              <select v-if="variable.options?.length" v-model="editor.fields![variable.name]"
                :data-variable-first="index === 0 || undefined">
                <option v-for="option in variable.options" :key="option" :value="option">{{ option }}</option>
              </select>
              <textarea v-else-if="variable.type === 'textarea'" v-model="editor.fields![variable.name]" rows="3"
                :data-variable-first="index === 0 || undefined"
                :placeholder="variable.placeholder || variable.default || variable.name"></textarea>
              <input v-else v-model="editor.fields![variable.name]"
                :data-variable-first="index === 0 || undefined"
                :type="variable.type === 'secret' ? 'password' : 'text'"
                :placeholder="variable.placeholder || variable.default || variable.name"
                :autocomplete="variable.type === 'secret' ? 'new-password' : 'off'" />
            </label>
          </div>
        </section>

        <label class="linked-secret">
          关联密码（可选）
          <input v-model="secretPassword" type="password" autocomplete="new-password"
            :placeholder="editor.secretId ? '已保存；留空表示不修改' : '独立加密保存，不写入模板'" />
          <small class="field-hint">密码单独加密保存，通过“复制密码”取用，不会自动进入命令或终端历史。</small>
        </label>

        <label class="sensitive-check"><input v-model="editor.sensitive" type="checkbox" /> 标记为敏感内容，提醒自己不要粘贴到聊天或日志</label>

        <p v-if="errorMessage" class="error">{{ errorMessage }}</p>
        <footer>
          <button v-if="editor.id" type="button" class="danger" @click="removeShortcut(editor)">删除</button>
          <span></span>
          <button type="button" @click="editorOpen = false">取消</button>
          <button class="primary" :disabled="!editor.trigger || !editor.title">加密保存</button>
        </footer>
      </form>
    </div>

    <div v-if="toast" class="toast"><i></i>{{ toast }}</div>
  </main>
</template>
