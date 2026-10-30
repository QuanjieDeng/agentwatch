// 导航的冒烟检查：node --test web/nav.test.mjs
// 房间尺寸与家具摆位跟 scene.js 保持一致。
import test from 'node:test';
import assert from 'node:assert/strict';
import { NavGrid } from './nav.js';

const W = 8, D = 8;
const DESK_X = [-2.4, -0.8, 0.8, 2.4];
const DESK_Z = [-0.9, 1.4];

function room() {
  const rects = [];
  for (const z of DESK_Z) for (const x of DESK_X) rects.push({ x, z, hw: 0.625, hd: 0.35 });
  rects.push({ x: 0, z: -2.3, hw: 0.625, hd: 0.35 });
  rects.push({ x: -3.5, z: -3.5, hw: 0.3, hd: 0.3 });
  rects.push({ x: 3.55, z: -3.55, hw: 0.3, hd: 0.3 });
  rects.push({ x: -3.55, z: 3.5, hw: 0.24, hd: 0.24 });
  rects.push({ x: -3.45, z: -0.9, hw: 0.24, hd: 0.24 });
  return new NavGrid(W, D, rects);
}

const door = { x: 3.1, z: 3.7 };
const targets = [];
for (const z of DESK_Z) {
  for (const x of DESK_X) {
    targets.push({ x, z: z - 0.78, name: `座位 ${x},${z}` });
    targets.push({ x: x + 0.85, z: z - 0.55, name: `站位 ${x},${z}` });
  }
}

test('从门口能走到每张桌子的座位和站位', () => {
  const nav = room();
  for (const to of targets) {
    const pts = nav.path(door, to);
    assert.ok(pts && pts.length > 0, `${to.name} 无路可达`);
    const last = pts[pts.length - 1];
    assert.ok(Math.hypot(last.x - to.x, last.z - to.z) < 1e-6, `${to.name} 没走到终点`);
  }
});

test('路径每一段都不穿过家具', () => {
  const nav = room();
  for (const to of targets) {
    const pts = nav.path(door, to);
    let from = door;
    for (const p of pts) {
      assert.ok(nav.clear(from, p), `${to.name} 的路线穿过了家具`);
      from = p;
    }
  }
});

test('挡路的桌子会让路线变长而不是直穿', () => {
  const nav = room();
  const from = { x: 0.8, z: 3.4 };
  const to = { x: 0.8, z: -1.68 }; // 正后方那张桌子的座位，直线会从桌洞里穿过
  const pts = nav.path(from, to);
  let len = 0;
  let prev = from;
  for (const p of pts) {
    len += Math.hypot(p.x - prev.x, p.z - prev.z);
    prev = p;
  }
  const straight = Math.hypot(to.x - from.x, to.z - from.z);
  assert.ok(len > straight + 0.2, `路线 ${len.toFixed(2)} 太短，疑似直穿（直线 ${straight.toFixed(2)}）`);
});

test('clamp 把人从家具里顶出来', () => {
  const nav = room();
  const p = { x: 0.8, z: 1.4 }; // 桌心
  nav.clamp(p);
  for (const o of nav.obstacles) {
    const inside = Math.abs(p.x - o.x) < o.hw + nav.radius && Math.abs(p.z - o.z) < o.hd + nav.radius;
    assert.ok(!inside, 'clamp 后仍在障碍内');
  }
});
