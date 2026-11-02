// 看板界面文案。语言优先级：URL ?lang= > 上次选择 > 浏览器语言 > 中文。
const KEY = 'agentwatch-lang';

export const DICT = {
  zh: {
    title: 'agentwatch · 办公室',
    connecting: '正在连接…',
    rooms: '房间',
    active: '活跃',
    stuck: '卡住',
    tokens: '总 token',
    resetView: '重置视角',
    unknownProject: '（未知项目）',
    mainAgent: '主 agent',
    closed: '（已关闭）',
    agents: n => `${n} 个 agent`,
    agentCount: n => `${n} 个`,
    noSessions: '当前没有会话',
    finished: '已结束',
    finishedRemoved: '已结束（已从服务器移除）',
    type: '类型',
    state: '状态',
    running: '已运行',
    turns: '轮次',
    input: '输入',
    output: '输出',
    cacheRead: '缓存读',
    noEvents: '暂无事件',
    kind_tool: '工具',
    kind_result: '结果',
    kind_thinking: '思考',
    kind_text: '文本',
    kind_user: '用户',
    toolFallback: '工具',
    toolReturned: tool => `${tool} 已返回，等待回应`,
    thinking: '思考中…',
    stuckFor: d => `卡住 ${d}`,
    sleeping: '呼呼大睡…',
    sec: n => `${n}秒`,
    minSec: (m, s) => `${m}分${s}秒`,
    hourMin: (h, m) => `${h}时${m}分`,
    errSnapshot: '快照格式不正确',
    errData: e => `数据处理失败：${e}`,
    errRobot: e => `角色模型加载失败：${e}`,
    errFetch: e => `无法获取快照：${e}`,
    errJson: '实时数据不是合法 JSON',
    errSse: '实时连接中断，正在重连…',
    errJoin: '；',
    emptyModel: '角色模型包围盒为空',
  },
  en: {
    title: 'agentwatch · Office',
    connecting: 'Connecting…',
    rooms: 'Rooms',
    active: 'Active',
    stuck: 'Stuck',
    tokens: 'Total tokens',
    resetView: 'Reset view',
    unknownProject: '(unknown project)',
    mainAgent: 'Main agent',
    closed: ' (closed)',
    agents: n => `${n} agent${n === 1 ? '' : 's'}`,
    agentCount: n => `${n} agent${n === 1 ? '' : 's'}`,
    noSessions: 'No sessions',
    finished: 'Finished',
    finishedRemoved: 'Finished (removed from server)',
    type: 'Type',
    state: 'State',
    running: 'Running for',
    turns: 'Turns',
    input: 'Input',
    output: 'Output',
    cacheRead: 'Cache read',
    noEvents: 'No events',
    kind_tool: 'Tool',
    kind_result: 'Result',
    kind_thinking: 'Thinking',
    kind_text: 'Text',
    kind_user: 'User',
    toolFallback: 'Tool',
    toolReturned: tool => `${tool} returned, awaiting response`,
    thinking: 'Thinking…',
    stuckFor: d => `Stuck ${d}`,
    sleeping: 'Sleeping…',
    sec: n => `${n}s`,
    minSec: (m, s) => `${m}m ${s}s`,
    hourMin: (h, m) => `${h}h ${m}m`,
    errSnapshot: 'Invalid snapshot format',
    errData: e => `Failed to process data: ${e}`,
    errRobot: e => `Failed to load the character model: ${e}`,
    errFetch: e => `Failed to fetch the snapshot: ${e}`,
    errJson: 'Live data is not valid JSON',
    errSse: 'Live connection lost, reconnecting…',
    errJoin: '; ',
    emptyModel: 'Character model has an empty bounding box',
  },
};

export let lang = 'zh';
let nf = new Intl.NumberFormat('zh-CN');

// 纯函数，方便 node 测试（不碰 location/localStorage/navigator）。
export function resolveLang(query, saved, nav) {
  if (query === 'zh' || query === 'en') return query;
  if (saved === 'zh' || saved === 'en') return saved;
  const navLang = String(nav || '').toLowerCase();
  if (navLang.startsWith('zh')) return 'zh';
  return navLang ? 'en' : 'zh';
}

export const locale = () => (lang === 'zh' ? 'zh-CN' : 'en-US');
export const fmtNum = n => nf.format(n);

export function tr(key, ...args) {
  const v = DICT[lang][key] ?? DICT.zh[key] ?? key;
  return typeof v === 'function' ? v(...args) : v;
}

// 主 agent 的 description 是服务端给的固定中文，界面统一按当前语言显示。
export const agentName = a => (a.id === 'main' ? tr('mainAgent') : a.description || a.type || a.id);

export function initLang() {
  const q = new URLSearchParams(location.search).get('lang');
  lang = resolveLang(q, localStorage.getItem(KEY), navigator.language);
  if (q === 'zh' || q === 'en') localStorage.setItem(KEY, q);
  nf = new Intl.NumberFormat(locale());
  document.documentElement.lang = locale();
  document.title = tr('title');
  for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = tr(el.dataset.i18n);
}
