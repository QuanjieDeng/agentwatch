import { createStage, RoomView, Flyers, roomSlot, roomsBox } from './scene.js';
import { Actor, loadRobot, robotReady, shortName, actionText, fmtDur } from './agents.js';

const $ = id => document.getElementById(id);
const errbar = $('errbar');
const nf = new Intl.NumberFormat('zh-CN');
const esc = s => String(s ?? '').replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
const KIND = { tool: '工具', result: '结果', thinking: '思考', text: '文本', user: '用户' };

const stage = createStage($('stage'));
const flyers = new Flyers(stage.scene);
const rooms = new Map(); // sessionId -> { view, actors: Map<key, Actor> }
// Page-lifetime history: the server forgets finished agents after 180s, the sidebar must not.
const history = new Map(); // key -> { key, sessionId, agent, gone }
const roomMeta = new Map(); // sessionId -> { name, alias, provider, status, gone }
let last = null;
let firstApply = true;
// Agents already present when the page opens appear in place; only later arrivals walk in from the door.
let actorsBuilt = false;
let layoutKey = '';
let selected = null;
let clockSkew = 0;
const errors = new Map();

const now = () => Date.now() + clockSkew;

function setError(slot, msg) {
  if (msg) errors.set(slot, msg); else errors.delete(slot);
  errbar.hidden = errors.size === 0;
  errbar.textContent = [...errors.values()].join('；');
}

function select(key) {
  if (!key || !history.has(key)) return;
  selected = key;
  for (const r of rooms.values()) for (const a of r.actors.values()) a.setSelected(a.key === key);
  renderSide(true);
}

function apply(snap) {
  if (!snap || !Array.isArray(snap.rooms)) throw new Error('快照格式不正确');
  last = snap;
  if (snap.generatedAt) clockSkew = snap.generatedAt - Date.now();
  $('proj').textContent = snap.project || '（未知项目）';

  for (const h of history.values()) h.gone = true;
  for (const m of roomMeta.values()) m.gone = true;

  const seen = new Set();
  snap.rooms.forEach((r, i) => {
    seen.add(r.sessionId);
    const list = [r.main, ...(r.agents ?? [])].filter(a => a && a.id);
    roomMeta.set(r.sessionId, { name: r.name || r.sessionId.slice(0, 8), alias: r.alias || '', provider: r.provider, status: r.status, gone: false });
    for (const a of list) history.set(`${r.sessionId}/${a.id}`, { key: `${r.sessionId}/${a.id}`, sessionId: r.sessionId, agent: a, gone: false });

    let room = rooms.get(r.sessionId);
    if (!room) {
      room = { view: new RoomView(stage.scene, select), actors: new Map() };
      room.view.group.position.copy(roomSlot(i));
      rooms.set(r.sessionId, room);
    }
    room.view.target.copy(roomSlot(i));
    room.view.setRoom(r, list.length);
    if (robotReady()) syncActors(room, r.sessionId, list);
  });
  if (robotReady()) actorsBuilt = true;

  for (const [sid, room] of rooms) {
    if (seen.has(sid)) continue;
    for (const a of room.actors.values()) a.dispose();
    room.view.dispose();
    rooms.delete(sid);
  }

  flyMessages(snap);

  const key = snap.rooms.map(r => r.sessionId).join('|');
  if (key !== layoutKey) {
    stage.frame(roomsBox(snap.rooms.length), firstApply);
    layoutKey = key;
  }
  firstApply = false;
  renderSide(false);
}

function syncActors(room, sid, list) {
  const live = new Set();
  for (const a of list) {
    const key = `${sid}/${a.id}`;
    live.add(key);
    let actor = room.actors.get(key);
    if (!actor) {
      actor = new Actor(key, a, room.view, !actorsBuilt, select);
      actor.setSelected(key === selected);
      room.view.group.add(actor.root);
      room.actors.set(key, actor);
    }
    actor.agent = a;
  }
  for (const [key, actor] of room.actors) {
    if (live.has(key)) continue;
    actor.dispose();
    room.actors.delete(key);
  }
  const needDesk = [...room.actors.values()]
    .filter(x => !x.desk && x.agent.state !== 'done' && !['farewell', 'leave', 'ghost'].includes(x.phase))
    .sort((x, y) => (x.agent.id < y.agent.id ? -1 : 1));
  for (const x of needDesk) x.setDesk(x.isMain ? room.view.foremanSpot : room.view.freeDesk());
  const t = now();
  for (const x of room.actors.values()) x.setAgent(x.agent, t);
}

// 消息收件人可以是同房间的 agent id / 描述，也可以是另一个会话的名字。
function findTarget(sid, to) {
  const room = rooms.get(sid);
  const local = room?.actors.get(`${sid}/${to}`);
  if (local) return local;
  if (room) for (const a of room.actors.values()) if (a.agent?.description === to) return a;
  for (const [id, r] of rooms) {
    const m = roomMeta.get(id);
    if (!m) continue;
    if (m.name === to || m.alias === to || (to.length > 3 && to.startsWith(m.name))) {
      return r.actors.get(`${id}/main`) ?? null;
    }
  }
  return null;
}

const seenMsgs = new Set();

function flyMessages(snap) {
  for (const r of snap.rooms) {
    const room = rooms.get(r.sessionId);
    if (!room) continue;
    // 房间首帧只记账：打开页面时不该把历史消息一次性补放成飞机群。
    const first = !room.primed;
    room.primed = true;
    for (const m of r.messages ?? []) {
      const key = `${r.sessionId}|${m.from}|${m.to}|${m.ts}`;
      if (seenMsgs.has(key)) continue;
      seenMsgs.add(key);
      if (first) continue;
      const from = room.actors.get(`${r.sessionId}/${m.from}`);
      const to = findTarget(r.sessionId, m.to);
      if (from && to) flyers.launch(from.root, to.root);
    }
  }
}

function totals() {
  let active = 0, stuck = 0, tokens = 0;
  for (const h of history.values()) {
    if (h.gone) continue;
    const a = h.agent;
    if (a.state === 'stuck') stuck++;
    else if (a.state === 'tool' || a.state === 'processing' || a.state === 'thinking') active++;
    tokens += (a.tokens?.in ?? 0) + (a.tokens?.out ?? 0);
  }
  return { active, stuck, tokens };
}

function elapsed(a, gone, t) {
  const end = a.finished || gone ? a.lastEventAt || t : t;
  return a.startedAt ? end - a.startedAt : NaN;
}

function rowHtml(h, t) {
  const a = h.agent;
  const state = h.gone ? 'done' : a.state;
  const cls = ['row', state === 'stuck' ? 'stuck' : '', h.gone || state === 'done' ? 'gone' : '', h.key === selected ? 'sel' : ''].join(' ');
  const act = h.gone ? '已结束' : actionText(a, t);
  return `<div class="${cls}" data-key="${esc(h.key)}">
    <span class="dot ${state}"></span>
    <span class="nm">${state === 'stuck' ? '⚠ ' : ''}${esc(a.id === 'main' ? '主 agent' : shortName(a))} <span class="ty">${esc(a.type)}</span></span>
    <span class="el">${fmtDur(elapsed(a, h.gone, t))}</span>
    <span class="ac">${esc(act)}</span>
  </div>`;
}

let eventsSig = '';

function renderSide(force) {
  const t = now();
  const { active, stuck, tokens } = totals();
  $('st-rooms').textContent = nf.format(last ? last.rooms.length : 0);
  $('st-active').textContent = nf.format(active);
  $('st-stuck').textContent = nf.format(stuck);
  $('st-stuck').classList.toggle('hot', stuck > 0);
  $('st-tokens').textContent = nf.format(tokens);

  const groups = new Map();
  for (const h of history.values()) {
    if (!groups.has(h.sessionId)) groups.set(h.sessionId, []);
    groups.get(h.sessionId).push(h);
  }
  let html = '';
  for (const [sid, list] of groups) {
    const m = roomMeta.get(sid);
    const shown = m.alias || m.name;
    const raw = m.alias && m.alias !== m.name ? `<span class="raw">${esc(m.name)}</span>` : '';
    const pv = m.provider === 'codex' ? '<span class="raw">Codex</span>' : '';
    html += `<div class="room-h"><span class="dot ${m.gone ? 'done' : m.status}"></span>${esc(shown)}${raw}${pv}${m.gone ? '（已关闭）' : ''}<span class="n">${list.length} 个</span></div>`;
    html += list.map(h => rowHtml(h, t)).join('');
  }
  $('list').innerHTML = html || '<div class="empty">当前没有会话</div>';

  const h = selected && history.get(selected);
  $('detail').hidden = !h;
  if (!h) return;
  const a = h.agent;
  const tk = a.tokens ?? {};
  $('detail-head').innerHTML = `<div class="d">${esc(a.description || a.id)}</div>
    <dl>
      <dt>类型</dt><dd>${esc(a.type)}</dd>
      <dt>状态</dt><dd>${h.gone ? '已结束（已从服务器移除）' : esc(actionText(a, t))}</dd>
      <dt>已运行</dt><dd>${fmtDur(elapsed(a, h.gone, t))}</dd>
      <dt>轮次</dt><dd>${nf.format(a.turns ?? 0)}</dd>
      <dt>tokens</dt><dd>输入 ${nf.format(tk.in ?? 0)} / 输出 ${nf.format(tk.out ?? 0)} / 缓存读 ${nf.format(tk.cacheRead ?? 0)}</dd>
    </dl>`;

  const evs = a.events ?? [];
  const sig = `${selected}|${evs.length}|${evs.at(-1)?.ts ?? 0}`;
  if (!force && sig === eventsSig) return;
  eventsSig = sig;
  const box = $('events');
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 24;
  box.innerHTML = evs.length
    ? evs.map(e => `<div class="ev ${esc(e.kind)}"><span class="ts">${new Date(e.ts).toLocaleTimeString('zh-CN', { hour12: false })}</span><span class="k">${KIND[e.kind] ?? esc(e.kind)}</span>${esc(e.text)}</div>`).join('')
    : '<div class="empty">暂无事件</div>';
  if (atBottom || force) box.scrollTop = box.scrollHeight;
}

$('list').addEventListener('click', e => select(e.target.closest('.row')?.dataset.key));
$('reset').addEventListener('click', () => stage.frame(roomsBox(last ? last.rooms.length : 1)));
setInterval(() => renderSide(false), 1000);

function safeApply(snap, slot) {
  try {
    apply(snap);
    setError(slot, null);
  } catch (err) {
    console.error(err);
    setError(slot, `数据处理失败：${err.message}`);
  }
}

loadRobot('/assets/RobotExpressive.glb')
  .then(() => last && safeApply(last, 'data'))
  .catch(err => { console.error(err); setError('robot', `角色模型加载失败：${err.message}`); });

fetch('/api/snapshot')
  .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.json(); })
  .then(snap => safeApply(snap, 'data'))
  .catch(err => setError('fetch', `无法获取快照：${err.message}`))
  .finally(() => {
    const es = new EventSource('/events');
    es.onmessage = e => {
      setError('sse', null);
      setError('fetch', null);
      let snap;
      try { snap = JSON.parse(e.data); } catch { setError('data', '实时数据不是合法 JSON'); return; }
      safeApply(snap, 'data');
    };
    es.onerror = () => setError('sse', '实时连接中断，正在重连…');
  });

const linkTargets = [];
let prevMs = performance.now();
function tick(ms = prevMs) {
  requestAnimationFrame(tick);
  const dt = Math.min((ms - prevMs) / 1000, 0.1);
  prevMs = ms;
  const t = ms / 1000;
  for (const { view, actors } of rooms.values()) {
    view.update(dt, t);
    linkTargets.length = 0;
    let boss = null;
    const list = [...actors.values()];
    for (const a of list) {
      a.update(dt, t, list);
      if (a.isMain) boss = a;
      else if (a.desk && a.phase !== 'ghost' && a.phase !== 'leave' && a.phase !== 'farewell') linkTargets.push(a.desk.seat);
    }
    view.updateLinks(boss ? boss.root.position : view.foremanSpot.stand, boss ? linkTargets : []);
  }
  flyers.update(dt);
  stage.render(dt);
}
tick();
