const profiles = [{ id: 'preview-profile', name: 'florune', createdAt: '', lastUsedAt: '' }]
const shortcuts = [
  {
    id: 'mysql-prod',
    trigger: ':mysql-connect-prod',
    title: 'MySQL 生产连接',
    category: 'mysql',
    template: 'mysql --host={{MYSQL_HOST}} --port={{MYSQL_PORT}} --user={{MYSQL_USER}} -p {{MYSQL_DATABASE}}',
    variables: [
      { name: 'MYSQL_HOST', label: '主机', type: 'text', default: 'MYSQL_HOST' },
      { name: 'MYSQL_PORT', label: '端口', type: 'text', default: '3306' },
      { name: 'MYSQL_USER', label: '用户', type: 'text', default: 'MYSQL_USER' },
      { name: 'MYSQL_DATABASE', label: '数据库', type: 'text', default: 'MYSQL_DATABASE' },
    ],
    fields: {
      MYSQL_HOST: 'db-prod.internal',
      MYSQL_PORT: '3306',
      MYSQL_USER: 'developer',
      MYSQL_DATABASE: 'orders',
    },
    content: 'mysql --host=db-prod.internal --port=3306 --user=developer -p orders',
    secretId: 'mysql-prod-password',
    updatedAt: '',
  },
  {
    id: 't2-restart',
    trigger: ':t2-restart',
    title: '重启 T2 服务',
    category: 'project',
    template: 'supervisorctl restart t2',
    content: 'supervisorctl restart t2',
    updatedAt: '',
  },
]
const entries = [
  {
    id: 'kafka-offset',
    trigger: ':kafka_offset',
    title: '查看 Kafka 消费组',
    description: '保存为自己的固定实例后无需再次填写。',
    category: 'kafka',
    template: 'kafka-consumer-groups.sh --bootstrap-server {{KAFKA_BOOTSTRAP_SERVERS}} --describe --group {{KAFKA_CONSUMER_GROUP}}',
    variables: [
      { name: 'KAFKA_BOOTSTRAP_SERVERS', label: 'Bootstrap Servers', type: 'text', default: 'KAFKA_BOOTSTRAP_SERVERS' },
      { name: 'KAFKA_CONSUMER_GROUP', label: '消费组', type: 'text', default: 'KAFKA_CONSUMER_GROUP' },
    ],
  },
]
const vault = {
  exists: true,
  unlocked: true,
  autoLockSeconds: 86400,
  remainingSeconds: 82800,
  storedSecretCount: 1,
  storedShortcutCount: shortcuts.length,
}

export function installBrowserPreview() {
  const browserWindow = window as any
  if (browserWindow.go?.main?.App) return
  const bootstrap = () => Promise.resolve({
    entries,
    profiles,
    activeProfile: profiles[0],
    vault,
    shortcuts,
    secrets: [],
    profileRoot: '%APPDATA%\\expand',
    vaultFile: '%APPDATA%\\expand\\profiles\\preview-profile\\vault.enc',
    logFile: '%LOCALAPPDATA%\\expand\\logs\\expand.log',
    hotkeyAvailable: true,
    hotkeyMessage: 'Ctrl + Alt + J',
  })
  browserWindow.go = {
    main: {
      App: {
        Bootstrap: bootstrap,
        CreateProfile: bootstrap,
        UnlockProfile: bootstrap,
        LockProfile: () => Promise.resolve({ ...(bootstrap as any)(), vault: { ...vault, unlocked: false } }),
        SwitchProfile: () => Promise.resolve({ entries, profiles, vault: { ...vault, unlocked: false }, shortcuts: [] }),
        SaveShortcut: (item: any) => Promise.resolve({ ...item, id: item.id || `preview-${Date.now()}`, updatedAt: '' }),
        DeleteShortcut: () => Promise.resolve(),
        UseShortcut: () => Promise.resolve(),
        CopySecret: () => Promise.resolve(),
        CopyText: () => Promise.resolve(),
        FrontendReady: () => Promise.resolve(),
        FrontendLog: () => Promise.resolve(),
        RenderEntry: () => Promise.resolve('preview output'),
        RenderShortcut: (id: string) => Promise.resolve(shortcuts.find((item) => item.id === id)?.content || ''),
        InsertText: () => Promise.resolve(),
        OpenManager: () => Promise.resolve(),
        OpenCompact: () => Promise.resolve(),
        HideWindow: () => Promise.resolve(),
        Quit: () => Promise.resolve(),
      },
    },
  }
  browserWindow.runtime = {
    EventsOnMultiple: () => () => undefined,
    EventsOff: () => undefined,
    EventsOffAll: () => undefined,
    EventsEmit: () => undefined,
  }
}
