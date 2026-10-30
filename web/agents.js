import * as THREE from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import * as SkeletonUtils from 'three/addons/utils/SkeletonUtils.js';
import { CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { tr } from './i18n.js';

const PALETTE = ['#8ec5e8', '#f4a7b9', '#a8d8b9', '#c9b6e4', '#f7c59f', '#9fd8d3', '#e8c99a', '#b5c8f0', '#f0b5d1', '#c4dca0'];
const FOREMAN = '#e9894f';
const GHOST = new THREE.Color('#a4a4a4');
const HEIGHT = 1.5;
const SPEED = 1.7;
const SEP = 0.6; // 角色之间的避让半径
const ONCE = new Set(['Sitting', 'Standing', 'Wave', 'ThumbsUp', 'Jump', 'Punch', 'Death', 'WalkJump']);
const OVERLAY_BONES = ['Hips', 'Abdomen', 'Head', 'UpperArm.R', 'LowerArm.L', 'LowerArm.R', 'Palm1.R', 'Index.L', 'Index.R', 'Middle1.L', 'Middle1.R'];

// Server state -> actor phase. farewell/leave/ghost are reached only through done.
const PHASE = { tool: 'work', processing: 'process', thinking: 'think', stuck: 'stuck', idle: 'sleep', done: 'farewell' };
const TERMINAL = new Set(['farewell', 'leave', 'ghost']);

let tpl = null;

export async function loadRobot(url) {
  const gltf = await new GLTFLoader().loadAsync(url);
  gltf.scene.updateMatrixWorld(true);
  const box = new THREE.Box3().setFromObject(gltf.scene);
  const h = box.max.y - box.min.y;
  if (!(h > 0)) throw new Error(tr('emptyModel'));
  tpl = { scene: gltf.scene, clips: gltf.animations, scale: HEIGHT / h, foot: -box.min.y };
}
export const robotReady = () => tpl !== null;

export function hashStr(s) {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

export function fmtDur(ms) {
  if (!(ms >= 0)) return '—';
  const s = Math.floor(ms / 1000);
  if (s < 60) return tr('sec', s);
  if (s < 3600) return tr('minSec', Math.floor(s / 60), s % 60);
  return tr('hourMin', Math.floor(s / 3600), Math.floor(s / 60) % 60);
}

const trunc = (s, n) => (s.length > n ? s.slice(0, n) + '…' : s);

export const shortName = a => trunc(a.description || a.type || a.id, 18);

export function actionText(a, now) {
  switch (a.state) {
    case 'tool': return trunc(`${a.tool} ${a.detail}`.trim(), 48);
    case 'processing': return tr('toolReturned', a.tool || tr('toolFallback'));
    case 'thinking': return tr('thinking');
    case 'stuck': return tr('stuckFor', fmtDur(now - a.lastEventAt));
    case 'done': return tr('finished');
    default: return tr('sleeping');
  }
}

const ringGeo = new THREE.RingGeometry(0.42, 0.56, 40).rotateX(-Math.PI / 2);
const selGeo = new THREE.RingGeometry(0.66, 0.76, 40).rotateX(-Math.PI / 2);
const selMat = new THREE.MeshBasicMaterial({ color: new THREE.Color('#fff2c0').multiplyScalar(1.6), toneMapped: false, transparent: true, depthWrite: false });
const GOLD = new THREE.Color('#ffc861').multiplyScalar(1.5);
const RED = new THREE.Color('#ff3b30');
const AMBER = new THREE.Color('#ffb347');
const OFF = new THREE.Color('#000000');

let bangMat = null;
function bang() {
  if (!bangMat) {
    const c = document.createElement('canvas');
    c.width = c.height = 64;
    const g = c.getContext('2d');
    g.fillStyle = '#e53935';
    g.beginPath(); g.arc(32, 32, 30, 0, Math.PI * 2); g.fill();
    g.fillStyle = '#fff';
    g.font = 'bold 46px sans-serif';
    g.textAlign = 'center'; g.textBaseline = 'middle';
    g.fillText('!', 32, 35);
    const tex = new THREE.CanvasTexture(c);
    tex.colorSpace = THREE.SRGBColorSpace;
    bangMat = new THREE.SpriteMaterial({ map: tex, toneMapped: false, depthTest: false });
  }
  const s = new THREE.Sprite(bangMat);
  s.scale.setScalar(0.42);
  s.renderOrder = 10;
  return s;
}

let zzzMat = null;
function zzz() {
  if (!zzzMat) {
    const c = document.createElement('canvas');
    c.width = 192; c.height = 72;
    const g = c.getContext('2d');
    g.fillStyle = 'rgba(58, 48, 38, .62)';
    g.beginPath();
    g.roundRect(8, 8, 176, 56, 26);
    g.fill();
    g.fillStyle = '#fff';
    g.font = 'bold 40px sans-serif';
    g.textAlign = 'center';
    g.textBaseline = 'middle';
    g.fillText('Z z z', 96, 37);
    const tex = new THREE.CanvasTexture(c);
    tex.colorSpace = THREE.SRGBColorSpace;
    zzzMat = new THREE.SpriteMaterial({ map: tex, toneMapped: false, depthTest: false });
  }
  const s = new THREE.Sprite(zzzMat);
  s.scale.set(1.0, 0.5, 1);
  s.renderOrder = 9;
  return s;
}

const tmp = new THREE.Vector3();

export class Actor {
  // room provides door / ghostSpot(); desk is {seat, stand, yaw, lamp, owner} or null.
  constructor(key, agent, room, spawnInPlace, onPick) {
    this.key = key;
    this.room = room;
    this.isMain = agent.id === 'main';
    this.seed = hashStr(key);
    this.desk = null;
    this.phase = null;
    this.want = null;
    this.path = null;
    this.working = false;
    this.sleeping = false;
    this.poseBase = null;
    this.poseStart = 0;
    this.timer = 0;
    this.yaw = Math.PI / 4;
    this.teleport = spawnInPlace;
    this.selected = false;

    const k = this.isMain ? 1.25 : 1;
    this.height = HEIGHT * k;
    this.root = new THREE.Group();
    this.body = SkeletonUtils.clone(tpl.scene);
    this.body.scale.setScalar(tpl.scale * k);
    this.body.position.y = tpl.foot * tpl.scale * k;
    const tint = new THREE.Color(this.isMain ? FOREMAN : PALETTE[this.seed % PALETTE.length]);
    const cloned = new Map();
    this.mats = [];
    this.body.traverse(o => {
      if (!o.isMesh) return;
      o.castShadow = true;
      let m = cloned.get(o.material);
      if (!m) {
        m = o.material.clone();
        if (m.name === 'Main') m.color.copy(tint);
        m.userData.base = m.color.clone();
        cloned.set(o.material, m);
        this.mats.push(m);
      }
      o.material = m;
    });
    this.root.add(this.body);
    this.bones = {};
    this.body.traverse(o => { if (o.isBone) this.bones[o.name] = o; });
    this.mixer = new THREE.AnimationMixer(this.body);
    this.action = null;
    this.anim = '';

    this.ringMat = new THREE.MeshBasicMaterial({ color: GOLD.clone(), toneMapped: false, transparent: true, depthWrite: false });
    this.ring = new THREE.Mesh(ringGeo, this.ringMat);
    this.ring.position.y = 0.02;
    this.ring.scale.setScalar(k);
    this.selRing = new THREE.Mesh(selGeo, selMat);
    this.selRing.position.y = 0.025;
    this.selRing.scale.setScalar(k);
    this.bang = bang();
    this.bang.position.set(0.45, this.height + 0.15, 0);
    this.zzz = zzz();
    this.zzz.visible = false;
    this.root.add(this.ring, this.selRing, this.bang, this.zzz);

    const el = document.createElement('div');
    el.innerHTML = '<div class="t1"></div><div class="t2"></div>';
    el.addEventListener('pointerdown', e => { e.stopPropagation(); onPick(key); });
    this.el = el;
    this.label = new CSS2DObject(el);
    this.label.center.set(0.5, 1);
    // The foreman stands behind the back-row desks, so on screen its label lands on theirs unless lifted.
    this.label.position.y = this.height + (this.isMain ? 0.95 : 0.3);
    this.root.add(this.label);

    this.root.position.copy(room.door);
  }

  setAgent(a, now) {
    this.agent = a;
    this.el.className = `tag ${a.state}${this.isMain ? ' main' : ''}${this.selected ? ' sel' : ''}`;
    this.el.children[0].textContent = shortName(a);
    this.el.children[1].textContent = actionText(a, now);
    if (this.teleport && a.state === 'done') {
      this.want = 'done';
      this.releaseDesk();
      this.root.position.copy(this.ghostSpot());
      this.becomeGhost();
    } else if (a.state !== this.want) {
      this.want = a.state;
      this.apply();
    }
    this.teleport = false;
  }

  setDesk(d) {
    if (this.desk === d) return;
    this.desk = d;
    if (d) d.owner = this;
    if (['work', 'process', 'think', 'stuck'].includes(this.phase)) {
      this.phase = null;
      this.apply();
    }
  }

  releaseDesk() {
    if (!this.desk) return;
    if (this.desk.lamp) this.desk.lamp.emissive.copy(OFF);
    this.desk.owner = null;
    this.desk = null;
  }

  setSelected(on) {
    this.selected = on;
    this.el.classList.toggle('sel', on);
  }

  apply() {
    if (TERMINAL.has(this.phase)) return;
    const phase = PHASE[this.want] ?? 'sleep';
    if (phase === this.phase) return;
    this.phase = phase;
    this.working = false;
    this.sleeping = false;
    this.poseStart = 0;
    this.zzz.visible = false;
    this[phase]();
  }

  spot() {
    return this.desk ?? { seat: this.root.position.clone(), stand: this.root.position.clone(), yaw: this.yaw };
  }

  work() {
    const d = this.spot();
    // ponytail: agents beyond the room's desk count stand instead of sitting; add desks if rooms routinely exceed 8.
    const sit = !!this.desk;
    this.walk(d.seat, () => {
      this.yaw = d.yaw;
      this.play(sit ? 'Sitting' : 'Standing');
      this.working = sit;
    });
  }

  process() {
    const d = this.spot();
    this.walk(d.stand, () => { this.yaw = d.yaw; this.play('Yes'); });
  }

  think() {
    const d = this.spot();
    this.walk(d.stand, () => { this.yaw = d.yaw; this.play('Idle'); });
  }

  stuck() {
    const d = this.spot();
    this.walk(d.stand, () => { this.yaw = d.yaw; this.play('No'); });
  }

  sleep() {
    const d = this.spot();
    if (!this.desk) {
      this.walk(d.stand, () => { this.yaw = d.yaw; this.play('Standing'); });
      return;
    }
    this.walk(d.seat, () => {
      this.yaw = d.yaw + 1.45; // 近乎横躺，沿墙倒而不是往墙里倒
      this.play('Sitting');
      this.sleeping = true;
    });
  }

  farewell() {
    this.releaseDesk();
    this.path = null;
    this.play(this.seed % 2 ? 'Wave' : 'ThumbsUp');
    this.timer = 1.8;
  }

  leave() {
    this.phase = 'leave';
    for (const m of this.mats) { m.transparent = true; m.needsUpdate = true; }
    const to = this.ghostSpot();
    this.leaveDist = Math.max(0.01, tmp.subVectors(to, this.root.position).length());
    this.walk(to, () => this.becomeGhost());
  }

  becomeGhost() {
    this.phase = 'ghost';
    this.path = null;
    for (const m of this.mats) { m.transparent = true; m.needsUpdate = true; }
    this.fade(1);
    this.body.traverse(o => { if (o.isMesh) o.castShadow = false; });
    this.yaw = Math.PI / 4;
    this.play('Idle');
  }

  ghostSpot() {
    return this.room.ghostSpot(this.seed);
  }

  fade(k) {
    for (const m of this.mats) {
      m.color.copy(m.userData.base).lerp(GHOST, k);
      m.opacity = 1 - 0.75 * k;
    }
  }

  walk(to, then) {
    tmp.subVectors(to, this.root.position);
    tmp.y = 0;
    if (this.teleport || tmp.length() < 0.05) {
      this.root.position.copy(to);
      this.path = null;
      then();
      return;
    }
    const pts = this.room.navPath(this.root.position, to) ?? [to.clone()];
    this.path = { pts, i: 0, then };
    this.play('Walking');
  }

  play(name) {
    if (this.anim === name) return;
    const clip = THREE.AnimationClip.findByName(tpl.clips, name);
    if (!clip) return;
    const next = this.mixer.clipAction(clip);
    const once = ONCE.has(name);
    next.reset();
    next.setLoop(once ? THREE.LoopOnce : THREE.LoopRepeat);
    next.clampWhenFinished = once;
    next.fadeIn(0.25).play();
    if (this.action) this.action.fadeOut(0.25);
    this.action = next;
    this.anim = name;
  }

  update(dt, t, others = []) {
    this.mixer.update(dt);
    const p = this.path;
    if (p) {
      const goal = p.pts[p.i];
      tmp.subVectors(goal, this.root.position);
      tmp.y = 0;
      const len = tmp.length();
      const step = SPEED * dt;
      // 互相避让：离得近就往外推；两边都在走各让一半，站着不动的只把走动的一方推开。
      let sx = 0, sz = 0;
      for (const o of others) {
        if (o === this || o.phase === 'ghost') continue;
        const dx = this.root.position.x - o.root.position.x;
        const dz = this.root.position.z - o.root.position.z;
        const d2 = dx * dx + dz * dz;
        if (d2 > SEP * SEP || d2 < 1e-6) continue;
        const d = Math.sqrt(d2);
        const w = ((SEP - d) / SEP) * (o.path ? 0.5 : 1);
        sx += (dx / d) * w;
        sz += (dz / d) * w;
      }
      const mx = (len > 1e-4 ? (tmp.x / len) * step : 0) + sx * step;
      const mz = (len > 1e-4 ? (tmp.z / len) * step : 0) + sz * step;
      this.root.position.x += mx;
      this.root.position.z += mz;
      this.room.clampPos(this.root.position);
      if (Math.abs(mx) + Math.abs(mz) > 1e-4) this.yaw = Math.atan2(mx, mz);
      if (len <= step) {
        this.root.position.copy(goal);
        this.room.clampPos(this.root.position);
        if (++p.i >= p.pts.length) {
          this.path = null;
          p.then();
        }
      }
      if (this.phase === 'leave') {
        const end = p.pts[p.pts.length - 1];
        const rest = Math.hypot(end.x - this.root.position.x, end.z - this.root.position.z);
        this.fade(1 - Math.min(1, rest / this.leaveDist));
      }
    }

    const arrived = !this.path;
    let yaw = this.yaw;
    if (this.phase === 'think' && arrived) yaw += Math.sin(t * 1.6 + this.seed) * 0.35;
    const r = this.root.rotation;
    r.y += Math.atan2(Math.sin(yaw - r.y), Math.cos(yaw - r.y)) * Math.min(1, dt * 8);

    this.pose(arrived, t);
    if (this.phase === 'farewell' && (this.timer -= dt) <= 0) this.leave();

    const stuck = this.phase === 'stuck';
    const pulse = 0.5 + 0.5 * Math.sin(t * 6);
    this.ring.visible = this.isMain || stuck;
    if (stuck) {
      this.ringMat.color.copy(RED).multiplyScalar(0.8 + pulse * 1.2);
      this.ringMat.opacity = 0.5 + pulse * 0.5;
    } else {
      this.ringMat.color.copy(GOLD);
      this.ringMat.opacity = 0.9;
    }
    this.bang.visible = stuck;
    this.bang.position.y = this.height + 0.15 + Math.sin(t * 4) * 0.05;
    this.selRing.visible = this.selected;
    this.root.scale.setScalar(this.selected ? 1.08 : 1);

    const lamp = this.desk?.lamp;
    if (lamp) {
      if (stuck) lamp.emissive.copy(RED).multiplyScalar(0.6 + pulse * 2.4);
      else if (this.phase === 'work' && arrived) lamp.emissive.copy(AMBER).multiplyScalar(2.2);
      else lamp.emissive.copy(OFF);
    }
  }

  // The mixer only rewrites bones that have tracks in the current clip. Deltas applied to a bare bone would
  // accumulate into a spin, so capture the settled seated pose once and restore it before every frame's offset.
  pose(arrived, t) {
    const B = this.bones;
    // 起身也必须还原：Hips / Abdomen 在所有 clip 里都没有轨道，离开坐姿后没人再写它们，
    // 残留的 -0.75 会一直跟着角色，下一轮睡姿还会以残留值为基准继续累加。
    if (this.poseBase && !this.sleeping && !this.working) {
      for (const n in this.poseBase) B[n].rotation.copy(this.poseBase[n]);
      this.poseBase = null;
    }
    if (!arrived) { this.zzz.visible = false; return; }
    if (this.sleeping || this.working) {
      if (!this.poseStart) this.poseStart = t;
      if (!this.poseBase && t - this.poseStart >= 1.0) {
        this.poseBase = {};
        for (const n of OVERLAY_BONES) if (B[n]) this.poseBase[n] = B[n].rotation.clone();
      }
      if (this.poseBase) for (const n in this.poseBase) B[n].rotation.copy(this.poseBase[n]);
    }
    if (this.sleeping) {
      if (this.poseBase) {
        if (B['Hips']) {
          B['Hips'].rotation.x -= 0.75;
          B['Hips'].rotation.z += 0.18;
        }
        if (B['Abdomen']) B['Abdomen'].rotation.x += Math.sin(t * 1.4) * 0.035;
        if (B['Head']) B['Head'].rotation.x += Math.sin(t * 0.9) * 0.06;
      }
      this.zzz.visible = true;
      const zy = this.height + (this.isMain ? 1.7 : 0.95) + Math.sin(t * 1.3) * 0.07;
      this.zzz.position.set(0.85 + Math.sin(t * 0.7) * 0.12, zy, 0);
      this.zzz.scale.set(1.9 + 0.1 * Math.sin(t * 1.3), 0.71, 1);
      return;
    }
    this.zzz.visible = false;
    if (!this.working || !this.poseBase) return;
    const cyc = t % 7;
    if (cyc < 4.6) {
      if (B['Abdomen']) B['Abdomen'].rotation.x += Math.sin(t * 7) * 0.02;
      if (B['LowerArm.L']) B['LowerArm.L'].rotation.x += Math.sin(t * 15) * 0.12;
      if (B['LowerArm.R']) B['LowerArm.R'].rotation.x += Math.sin(t * 15 + 1.9) * 0.12;
      if (B['Index.L']) B['Index.L'].rotation.x += Math.sin(t * 21) * 0.22;
      if (B['Index.R']) B['Index.R'].rotation.x += Math.sin(t * 21 + 2.2) * 0.22;
      if (B['Middle1.L']) B['Middle1.L'].rotation.x += Math.sin(t * 23 + 0.7) * 0.18;
      if (B['Middle1.R']) B['Middle1.R'].rotation.x += Math.sin(t * 23 + 2.9) * 0.18;
      return;
    }
    const k = Math.min(1, (cyc - 4.6) / 0.4, (7 - cyc) / 0.4);
    if (B['UpperArm.R']) B['UpperArm.R'].rotation.x += 0.45 * k;
    if (B['LowerArm.R']) B['LowerArm.R'].rotation.z += 0.25 * k;
    if (B['Palm1.R']) {
      B['Palm1.R'].rotation.y += Math.sin(t * 5) * 0.18 * k;
      B['Palm1.R'].rotation.x += Math.cos(t * 5) * 0.12 * k;
    }
  }

  dispose() {
    this.releaseDesk();
    // CSS2DRenderer only drops the DOM node when the label itself is removed.
    this.label.removeFromParent();
    this.mixer.stopAllAction();
    this.mixer.uncacheRoot(this.body);
    for (const m of this.mats) m.dispose();
    this.ringMat.dispose();
    this.root.removeFromParent();
  }
}
