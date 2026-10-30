import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js';
import { RoundedBoxGeometry } from 'three/addons/geometries/RoundedBoxGeometry.js';
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js';
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js';
import { CSS2DRenderer, CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { NavGrid } from './nav.js';

// Room footprint in local units; the camera looks from +x/+z so the walls sit at -x and -z.
const W = 8, D = 8, HW = W / 2, HD = D / 2;
const PITCH_X = 10.5, PITCH_Z = 9.5, PER_ROW = 3;
const VIEW_DIR = new THREE.Vector3(1, 1.05, 1).normalize();
const DESK_X = [-2.4, -0.8, 0.8, 2.4];
const DESK_Z = [-0.9, 1.4];

export function createStage(host) {
  const renderer = new THREE.WebGLRenderer({ antialias: true });
  renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
  renderer.shadowMap.enabled = true;
  // The vendored r186 removed PCFSoftShadowMap and downgrades it to PCF with a warning every load.
  renderer.shadowMap.type = THREE.PCFShadowMap;
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 0.9;
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  host.appendChild(renderer.domElement);

  const labels = new CSS2DRenderer();
  labels.domElement.className = 'label-layer';
  Object.assign(labels.domElement.style, { position: 'absolute', top: '0', left: '0' });
  host.appendChild(labels.domElement);

  const scene = new THREE.Scene();
  scene.background = gradientTexture();
  const pmrem = new THREE.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.environmentIntensity = 0.3;
  pmrem.dispose();

  scene.add(new THREE.HemisphereLight('#fff4e6', '#b89478', 0.55));
  const sun = new THREE.DirectionalLight('#fff1dc', 1.6);
  sun.castShadow = true;
  sun.shadow.mapSize.set(2048, 2048);
  sun.shadow.bias = -0.0004;
  sun.shadow.normalBias = 0.02;
  sun.shadow.radius = 3;
  scene.add(sun, sun.target);

  const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 400);
  // The label layer is pointer-events:none so drags reach the canvas; tags opt back in individually.
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.08;
  controls.maxPolarAngle = Math.PI * 0.46;
  controls.minDistance = 4;
  controls.maxDistance = 160;

  const composer = new EffectComposer(renderer);
  composer.addPass(new RenderPass(scene, camera));
  const bloom = new UnrealBloomPass(new THREE.Vector2(1, 1), 0.35, 0.4, 1.0);
  composer.addPass(bloom);
  composer.addPass(new OutputPass());

  const fly = { t: 1, fromP: new THREE.Vector3(), fromT: new THREE.Vector3(), toP: new THREE.Vector3(), toT: new THREE.Vector3() };
  let frameBox = new THREE.Box3(new THREE.Vector3(-HW, 0, -HD), new THREE.Vector3(HW, 2, HD));

  function resize() {
    const w = host.clientWidth, h = host.clientHeight;
    renderer.setSize(w, h);
    composer.setSize(w, h);
    labels.setSize(w, h);
    camera.aspect = w / h;
    camera.updateProjectionMatrix();
  }

  // Fit the box's bounding sphere into the narrower of the two FOVs, then fly there.
  function frame(box = frameBox, instant = false) {
    frameBox = box.clone();
    const sphere = box.getBoundingSphere(new THREE.Sphere());
    const vfov = THREE.MathUtils.degToRad(camera.fov);
    const hfov = 2 * Math.atan(Math.tan(vfov / 2) * camera.aspect);
    const dist = sphere.radius / Math.sin(Math.min(vfov, hfov) / 2) * 0.82;
    fly.fromP.copy(camera.position);
    fly.fromT.copy(controls.target);
    fly.toT.copy(sphere.center).setY(0.4);
    fly.toP.copy(fly.toT).addScaledVector(VIEW_DIR, dist);
    fly.t = instant ? 1 : 0;
    if (instant) { camera.position.copy(fly.toP); controls.target.copy(fly.toT); }

    const r = sphere.radius + 4;
    sun.position.copy(sphere.center).add(new THREE.Vector3(r * 0.6, r * 1.2, r * 0.35));
    sun.target.position.copy(sphere.center);
    Object.assign(sun.shadow.camera, { left: -r, right: r, top: r, bottom: -r, near: 0.5, far: r * 3.5 });
    sun.shadow.camera.updateProjectionMatrix();
  }

  function render(dt) {
    if (fly.t < 1) {
      fly.t = Math.min(1, fly.t + dt / 0.9);
      const k = 1 - Math.pow(1 - fly.t, 3);
      camera.position.lerpVectors(fly.fromP, fly.toP, k);
      controls.target.lerpVectors(fly.fromT, fly.toT, k);
    }
    controls.update();
    composer.render(dt);
    labels.render(scene, camera);
  }

  addEventListener('resize', () => { resize(); });
  resize();
  frame(frameBox, true);
  return { scene, camera, render, frame, resize };
}

function gradientTexture() {
  const c = document.createElement('canvas');
  c.width = 4; c.height = 256;
  const g = c.getContext('2d');
  const grad = g.createLinearGradient(0, 0, 0, 256);
  grad.addColorStop(0, '#fdf1e2');
  grad.addColorStop(0.6, '#f6dcc4');
  grad.addColorStop(1, '#ecc7a8');
  g.fillStyle = grad;
  g.fillRect(0, 0, 4, 256);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

export function roomSlot(i) {
  return new THREE.Vector3((i % PER_ROW) * PITCH_X, 0, Math.floor(i / PER_ROW) * PITCH_Z);
}

export function roomsBox(n) {
  const box = new THREE.Box3();
  for (let i = 0; i < Math.max(1, n); i++) {
    const p = roomSlot(i);
    box.expandByPoint(new THREE.Vector3(p.x - HW, 0, p.z - HD));
    box.expandByPoint(new THREE.Vector3(p.x + HW, 2.2, p.z + HD));
  }
  return box;
}

const std = (color, extra) => new THREE.MeshStandardMaterial({ color, roughness: 0.8, ...extra });
const M = {
  floor: std('#e9d3b4'), wall: std('#f6ece0'), wallTrim: std('#d9b99a'),
  rug: std('#c98f86'), rugEdge: std('#e7c1a2'),
  desk: std('#c99a6e'), deskLeg: std('#8c6a4f'), chair: std('#7f8fa6'), chairSeat: std('#5f6f86'),
  monitor: std('#3a3d44', { roughness: 0.4 }), keyboard: std('#d8d4ce'),
  screen: new THREE.MeshStandardMaterial({ color: '#202a36', emissive: new THREE.Color('#9fd3ff'), emissiveIntensity: 0.25, roughness: 0.3 }),
  pot: std('#d97b5c'), leaf: std('#7fae7a', { flatShading: true }), leaf2: std('#9cc48a', { flatShading: true }),
  pole: std('#4b4440', { roughness: 0.5 }), lampShade: new THREE.MeshStandardMaterial({ color: '#fff0d0', emissive: new THREE.Color('#ffcf8a'), emissiveIntensity: 2.4, toneMapped: false }),
  deskLampBody: std('#5b5550', { roughness: 0.5 }),
};
const G = {
  floor: new RoundedBoxGeometry(W, 0.3, D, 3, 0.14),
  wallBack: new RoundedBoxGeometry(W, 1.1, 0.18, 2, 0.06),
  wallLeft: new RoundedBoxGeometry(0.18, 1.1, D, 2, 0.06),
  trimBack: new THREE.BoxGeometry(W, 0.06, 0.24),
  trimLeft: new THREE.BoxGeometry(0.24, 0.06, D),
  rug: new RoundedBoxGeometry(6.4, 0.03, 4.6, 2, 0.012),
  rugEdge: new RoundedBoxGeometry(6.8, 0.02, 5.0, 2, 0.008),
  deskTop: new RoundedBoxGeometry(1.25, 0.06, 0.7, 2, 0.02),
  deskLeg: new THREE.BoxGeometry(0.05, 0.62, 0.05),
  monitor: new RoundedBoxGeometry(0.62, 0.38, 0.04, 2, 0.015),
  screen: new THREE.PlaneGeometry(0.56, 0.32),
  stand: new THREE.BoxGeometry(0.05, 0.16, 0.05),
  keyboard: new RoundedBoxGeometry(0.42, 0.02, 0.14, 1, 0.006),
  seat: new RoundedBoxGeometry(0.46, 0.07, 0.44, 2, 0.03),
  back: new RoundedBoxGeometry(0.46, 0.42, 0.06, 2, 0.03),
  chairLeg: new THREE.CylinderGeometry(0.03, 0.03, 0.38, 6),
  pot: new THREE.CylinderGeometry(0.22, 0.16, 0.36, 8),
  leaf: new THREE.IcosahedronGeometry(0.34, 0),
  pole: new THREE.CylinderGeometry(0.025, 0.025, 1.7, 6),
  base: new THREE.CylinderGeometry(0.2, 0.22, 0.04, 12),
  shade: new THREE.ConeGeometry(0.28, 0.3, 12, 1, true),
  deskLampArm: new THREE.CylinderGeometry(0.012, 0.012, 0.26, 5),
  deskLampHead: new THREE.SphereGeometry(0.07, 10, 6, 0, Math.PI * 2, 0, Math.PI / 2),
};

function mesh(geo, mat, x, y, z, cast = true) {
  const m = new THREE.Mesh(geo, mat);
  m.position.set(x, y, z);
  m.castShadow = cast;
  m.receiveShadow = true;
  return m;
}

function buildDesk(x, z) {
  const g = new THREE.Group();
  g.position.set(x, 0, z);
  g.add(mesh(G.deskTop, M.desk, 0, 0.66, 0));
  for (const [lx, lz] of [[-0.56, -0.29], [0.56, -0.29], [-0.56, 0.29], [0.56, 0.29]]) g.add(mesh(G.deskLeg, M.deskLeg, lx, 0.32, lz));
  // Monitor at the desk's +z edge facing -z, so the seated robot faces the camera over it.
  g.add(mesh(G.stand, M.deskLeg, 0, 0.77, 0.2));
  const mon = mesh(G.monitor, M.monitor, 0, 1.0, 0.22);
  const scr = new THREE.Mesh(G.screen, M.screen);
  scr.position.set(0, 0, -0.022);
  scr.rotation.y = Math.PI;
  mon.add(scr);
  g.add(mon);
  g.add(mesh(G.keyboard, M.keyboard, 0, 0.7, -0.05));
  const lampMat = new THREE.MeshStandardMaterial({ color: '#f3e3c8', emissive: new THREE.Color(0, 0, 0), toneMapped: false });
  g.add(mesh(G.deskLampArm, M.deskLampBody, 0.48, 0.81, 0.15));
  const head = mesh(G.deskLampHead, lampMat, 0.48, 0.92, 0.15);
  head.rotation.x = Math.PI;
  g.add(head);
  const chair = new THREE.Group();
  chair.position.set(0, 0, -0.68);
  chair.add(mesh(G.seat, M.chairSeat, 0, 0.42, 0));
  chair.add(mesh(G.back, M.chair, 0, 0.66, -0.22));
  chair.add(mesh(G.chairLeg, M.deskLeg, 0, 0.2, 0));
  g.add(chair);
  // Robot hips sit lower than the chair top in the Sitting clip, so the seat point is pushed back toward the backrest.
  return {
    group: g,
    seat: new THREE.Vector3(x, 0, z - 0.78),
    stand: new THREE.Vector3(x + 0.85, 0, z - 0.55),
    yaw: 0,
    lamp: lampMat,
    owner: null,
    // 桌面本身才是障碍：座位在桌后、站位在桌侧，都留在可行区域内。
    rect: { x, z, hw: 0.625, hd: 0.35 },
  };
}

function buildPlant(x, z, s = 1) {
  const g = new THREE.Group();
  g.position.set(x, 0, z);
  g.scale.setScalar(s);
  g.add(mesh(G.pot, M.pot, 0, 0.18, 0));
  g.add(mesh(G.leaf, M.leaf, 0, 0.55, 0));
  g.add(mesh(G.leaf, M.leaf2, 0.12, 0.8, 0.05).rotateY(0.6));
  return g;
}

function buildFloorLamp(x, z) {
  const g = new THREE.Group();
  g.position.set(x, 0, z);
  g.add(mesh(G.base, M.pole, 0, 0.02, 0));
  g.add(mesh(G.pole, M.pole, 0, 0.87, 0));
  g.add(mesh(G.shade, M.lampShade, 0, 1.78, 0, false));
  const light = new THREE.PointLight('#ffc98a', 3, 8, 1.6);
  light.position.set(0, 1.6, 0);
  g.add(light);
  return g;
}

export class RoomView {
  constructor(parent, onPick) {
    this.group = new THREE.Group();
    this.target = new THREE.Vector3();
    this.door = new THREE.Vector3(HW - 0.9, 0, HD - 0.3);
    const g = this.group;
    this.foremanSpot = buildDesk(0, -2.3);
    g.add(this.foremanSpot.group);
    g.add(mesh(G.floor, M.floor, 0, -0.15, 0, false));
    g.add(mesh(G.wallBack, M.wall, 0, 0.55, -HD + 0.09));
    g.add(mesh(G.wallLeft, M.wall, -HW + 0.09, 0.55, 0));
    g.add(mesh(G.trimBack, M.wallTrim, 0, 1.1, -HD + 0.09));
    g.add(mesh(G.trimLeft, M.wallTrim, -HW + 0.09, 1.1, 0));
    g.add(mesh(G.rugEdge, M.rugEdge, 0.2, 0.01, 0.25, false));
    g.add(mesh(G.rug, M.rug, 0.2, 0.02, 0.25, false));
    const grid = [];
    for (const z of DESK_Z) for (const x of DESK_X) {
      const d = buildDesk(x, z);
      g.add(d.group);
      grid.push(d);
    }
    // Hand out desks spread across the room so the first few agents' labels don't stack on each other.
    this.desks = [7, 4, 0, 3, 5, 6, 1, 2].map(i => grid[i]);
    g.add(buildPlant(-HW + 0.5, -HD + 0.5, 1.2), buildPlant(HW - 0.45, -HD + 0.45), buildPlant(-HW + 0.45, HD - 0.5, 0.9));
    g.add(buildFloorLamp(-HW + 0.55, -0.9));
    // 家具占的地面：桌子逐张登记，装饰件按外接圆放大成小方块。
    this.nav = new NavGrid(W, D, [
      ...grid.map(d => d.rect), this.foremanSpot.rect,
      { x: -HW + 0.5, z: -HD + 0.5, hw: 0.3, hd: 0.3 },
      { x: HW - 0.45, z: -HD + 0.45, hw: 0.3, hd: 0.3 },
      { x: -HW + 0.45, z: HD - 0.5, hw: 0.24, hd: 0.24 },
      { x: -HW + 0.55, z: -0.9, hw: 0.24, hd: 0.24 },
    ]);

    const el = document.createElement('div');
    el.className = 'nameplate';
    el.innerHTML = '<span class="dot"></span><span class="nm"></span><span class="n"></span>';
    el.addEventListener('pointerdown', e => { e.stopPropagation(); onPick(this.firstKey); });
    this.plateEl = el;
    this.plate = new CSS2DObject(el);
    this.plate.position.set(0, 0.1, HD + 0.35);
    g.add(this.plate);

    this.linkGeo = new THREE.BufferGeometry();
    this.linkGeo.setAttribute('position', new THREE.BufferAttribute(new Float32Array(32 * 24 * 2 * 3), 3).setUsage(THREE.DynamicDrawUsage));
    this.linkMat = new THREE.LineBasicMaterial({ color: new THREE.Color('#ffd27a').multiplyScalar(2.2), toneMapped: false, transparent: true, opacity: 0.85 });
    this.links = new THREE.LineSegments(this.linkGeo, this.linkMat);
    this.links.frustumCulled = false;
    g.add(this.links);
    parent.add(g);
  }

  setRoom(r, count) {
    this.firstKey = `${r.sessionId}/main`;
    this.plateEl.children[0].className = `dot ${r.status}`;
    this.plateEl.children[1].textContent = r.alias || r.name || r.sessionId.slice(0, 8);
    this.plateEl.children[1].title = r.alias && r.name ? r.name : '';
    this.plateEl.children[2].textContent = `${count} 个 agent${r.provider === 'codex' ? ' · Codex' : ''}`;
  }

  ghostSpot(seed) {
    return new THREE.Vector3(HW - 1.6 - (seed % 6) * 0.55, 0, HD - 0.45 - ((seed >> 3) % 2) * 0.5);
  }

  freeDesk() {
    return this.desks.find(d => !d.owner) ?? null;
  }

  // 绕开家具的折线；无解（比如两端都被围死）时返回 null，调用方退回直线。
  navPath(from, to) {
    const pts = this.nav.path(from, to);
    return pts && pts.map(p => new THREE.Vector3(p.x, 0, p.z));
  }

  clampPos(p) {
    this.nav.clamp(p);
  }

  // Quadratic arcs from the foreman's head to each linked desk, rewritten every frame since agents walk.
  updateLinks(from, targets) {
    const pos = this.linkGeo.attributes.position;
    const a = new THREE.Vector3(), b = new THREE.Vector3(), c = new THREE.Vector3(), p = new THREE.Vector3(), q = new THREE.Vector3();
    let n = 0;
    for (const t of targets.slice(0, 32)) {
      a.copy(from).setY(1.9);
      b.copy(t).setY(1.2);
      c.addVectors(a, b).multiplyScalar(0.5).setY(Math.max(a.y, b.y) + 0.9);
      p.copy(a);
      for (let i = 1; i <= 24; i++) {
        const s = i / 24, u = 1 - s;
        q.set(0, 0, 0).addScaledVector(a, u * u).addScaledVector(c, 2 * u * s).addScaledVector(b, s * s);
        pos.setXYZ(n++, p.x, p.y, p.z);
        pos.setXYZ(n++, q.x, q.y, q.z);
        p.copy(q);
      }
    }
    this.linkGeo.setDrawRange(0, n);
    pos.needsUpdate = true;
  }

  update(dt, t) {
    this.group.position.lerp(this.target, Math.min(1, dt * 4));
    this.linkMat.opacity = 0.6 + 0.25 * Math.sin(t * 2);
  }

  dispose() {
    this.plate.removeFromParent();
    for (const d of this.desks) d.lamp.dispose();
    this.linkGeo.dispose();
    this.linkMat.dispose();
    this.group.removeFromParent();
  }
}

// 一张纸飞机：两片机翼加一道龙骨，够看出轮廓即可。
const planeGeo = (() => {
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute([
    0, 0, 0.3, -0.17, 0.03, -0.2, 0, 0.05, -0.13, // 左翼
    0, 0, 0.3, 0, 0.05, -0.13, 0.17, 0.03, -0.2, // 右翼
    0, 0, 0.3, 0, -0.09, -0.14, 0, 0.05, -0.13,  // 龙骨
  ], 3));
  g.computeVertexNormals();
  return g;
})();
const UP = new THREE.Vector3(0, 1, 0);
const ORIGIN = new THREE.Vector3();

class PaperPlane {
  constructor(from, to) {
    this.a = from.clone().setY(from.y + 1.25);
    this.b = to.clone().setY(to.y + 1.25);
    const dist = this.a.distanceTo(this.b);
    this.c = this.a.clone().add(this.b).multiplyScalar(0.5).setY(Math.max(this.a.y, this.b.y) + Math.min(3, 0.5 + dist * 0.12));
    this.dur = Math.min(2.4, 0.7 + dist * 0.09);
    this.t = 0;
    this.p = new THREE.Vector3();
    this.q = new THREE.Vector3();
    this.mat = new THREE.MeshStandardMaterial({
      color: '#ffe8bd', emissive: new THREE.Color('#ff8a2b'), emissiveIntensity: 0.7,
      side: THREE.DoubleSide, flatShading: true, transparent: true, roughness: 0.7,
    });
    this.mesh = new THREE.Mesh(planeGeo, this.mat);
    this.mesh.scale.setScalar(2);
    this.mesh.position.copy(this.a);
  }

  at(k, out) {
    const u = 1 - k;
    return out.set(0, 0, 0).addScaledVector(this.a, u * u).addScaledVector(this.c, 2 * u * k).addScaledVector(this.b, k * k);
  }

  update(dt) {
    this.t = Math.min(1, this.t + dt / this.dur);
    this.at(this.t, this.p);
    this.mesh.position.copy(this.p);
    this.at(Math.min(1, this.t + 0.04), this.q).sub(this.p);
    if (this.q.lengthSq() > 1e-9) {
      // 方向直接按局部向量算：+z 对着前进方向，父节点只有平移，不用管世界坐标。
      this.q.normalize();
      this.mesh.quaternion.setFromRotationMatrix(new THREE.Matrix4().lookAt(this.q, ORIGIN, UP));
      this.mesh.rotateZ(Math.sin(this.t * Math.PI) * 0.5);
    }
    this.mat.opacity = this.t > 0.85 ? (1 - this.t) / 0.15 : 1;
    return this.t >= 1;
  }

  dispose() {
    this.mesh.removeFromParent();
    this.mat.dispose();
  }
}

// 纸飞机挂在 scene 上而不是房间组里，跨房间的消息也要能飞。
export class Flyers {
  constructor(parent) {
    this.parent = parent;
    this.items = [];
  }

  launch(fromObj, toObj) {
    const p = new PaperPlane(fromObj.getWorldPosition(new THREE.Vector3()), toObj.getWorldPosition(new THREE.Vector3()));
    this.parent.add(p.mesh);
    this.items.push(p);
  }

  update(dt) {
    for (let i = this.items.length - 1; i >= 0; i--) {
      if (!this.items[i].update(dt)) continue;
      this.items[i].dispose();
      this.items.splice(i, 1);
    }
  }
}
