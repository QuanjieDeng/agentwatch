// 房间内的地面导航：把家具挖成静态障碍，用格子图 + A* 出一条绕行的折线。
// 纯数据，不依赖 three；坐标都是地面平面上的 {x, z}。
const NEAR = [
  [1, 0, 1], [-1, 0, 1], [0, 1, 1], [0, -1, 1],
  [1, 1, Math.SQRT2], [1, -1, Math.SQRT2], [-1, 1, Math.SQRT2], [-1, -1, Math.SQRT2],
];

export class NavGrid {
  // obstacles 是轴对齐矩形 { x, z, hw, hd }；房间以原点为中心，宽 width 深 depth。
  constructor(width, depth, obstacles, cell = 0.2, radius = 0.15) {
    this.obstacles = obstacles;
    this.radius = radius;
    this.cell = cell;
    this.nx = Math.round(width / cell);
    this.nz = Math.round(depth / cell);
    this.x0 = -width / 2;
    this.z0 = -depth / 2;
    this.blocked = new Uint8Array(this.nx * this.nz);
    for (let j = 0; j < this.nz; j++) {
      for (let i = 0; i < this.nx; i++) {
        const x = this.x0 + i * cell;
        const z = this.z0 + j * cell;
        for (const o of obstacles) {
          // 按角色半径膨胀后再挖空，格子里的中心点才保证离家具足够远。
          if (x < o.x + o.hw + radius && x + cell > o.x - o.hw - radius
            && z < o.z + o.hd + radius && z + cell > o.z - o.hd - radius) {
            this.blocked[j * this.nx + i] = 1;
            break;
          }
        }
      }
    }
  }

  cellOf(p) {
    const i = Math.floor((p.x - this.x0) / this.cell);
    const j = Math.floor((p.z - this.z0) / this.cell);
    return [Math.min(this.nx - 1, Math.max(0, i)), Math.min(this.nz - 1, Math.max(0, j))];
  }

  // 起点/终点可能落在家具占的格子里（比如紧贴桌边），就近找一个空格当端点。
  openNear(i, j) {
    for (let r = 0; r < 12; r++) {
      for (let dj = -r; dj <= r; dj++) {
        for (let di = -r; di <= r; di++) {
          if (Math.max(Math.abs(di), Math.abs(dj)) !== r) continue;
          const a = i + di, b = j + dj;
          if (a < 0 || b < 0 || a >= this.nx || b >= this.nz) continue;
          if (!this.blocked[b * this.nx + a]) return [a, b];
        }
      }
    }
    return null;
  }

  // 8 邻接、不许斜穿桌角；格子总数只有几百，开放列表线性扫描足够（每次 walk 才查一次）。
  search(s, g) {
    const { nx, blocked } = this;
    const N = nx * this.nz;
    const cost = new Float32Array(N).fill(Infinity);
    const prev = new Int32Array(N).fill(-1);
    const start = s[1] * nx + s[0];
    const open = [start];
    const f = new Map([[start, 0]]);
    cost[start] = 0;
    while (open.length) {
      let best = 0;
      for (let k = 1; k < open.length; k++) if (f.get(open[k]) < f.get(open[best])) best = k;
      const cur = open.splice(best, 1)[0];
      f.delete(cur);
      const ci = cur % nx, cj = (cur - ci) / nx;
      if (ci === g[0] && cj === g[1]) {
        const out = [];
        for (let c = cur; c !== -1; c = prev[c]) out.push([c % nx, (c - (c % nx)) / nx]);
        return out.reverse();
      }
      for (const [di, dj, step] of NEAR) {
        const ai = ci + di, aj = cj + dj;
        if (ai < 0 || aj < 0 || ai >= nx || aj >= this.nz) continue;
        const n = aj * nx + ai;
        if (blocked[n]) continue;
        if (di && dj && (blocked[cj * nx + ai] || blocked[aj * nx + ci])) continue;
        const nc = cost[cur] + step;
        if (nc >= cost[n]) continue;
        cost[n] = nc;
        prev[n] = cur;
        f.set(n, nc + Math.hypot(ai - g[0], aj - g[1]));
        if (!open.includes(n)) open.push(n);
      }
    }
    return null;
  }

  // 两点之间能不能直走（按角色半径膨胀后的家具做采样）。
  clear(a, b) {
    const dx = b.x - a.x, dz = b.z - a.z;
    const n = Math.ceil(Math.hypot(dx, dz) / 0.1);
    for (let k = 1; k < n; k++) {
      const x = a.x + (dx * k) / n, z = a.z + (dz * k) / n;
      for (const o of this.obstacles) {
        if (Math.abs(x - o.x) < o.hw + this.radius && Math.abs(z - o.z) < o.hd + this.radius) return false;
      }
    }
    return true;
  }

  // 返回 [{x,z}, ...]，首点是起点之后的第一个拐点，末点是精确的 to；无路时返回 null。
  path(from, to) {
    const s = this.openNear(...this.cellOf(from));
    const g = this.openNear(...this.cellOf(to));
    if (!s || !g) return null;
    const cells = this.search(s, g);
    if (!cells) return null;
    const pts = [{ x: from.x, z: from.z }];
    for (const [i, j] of cells) {
      const x = this.x0 + (i + 0.5) * this.cell, z = this.z0 + (j + 0.5) * this.cell;
      const last = pts[pts.length - 1];
      if (Math.hypot(x - last.x, z - last.z) > 0.05) pts.push({ x, z });
    }
    const last = pts[pts.length - 1];
    if (Math.hypot(to.x - last.x, to.z - last.z) < 0.05) {
      last.x = to.x;
      last.z = to.z;
    } else {
      pts.push({ x: to.x, z: to.z });
    }
    // 拉直：从当前点直接跳到还能直视的最远点，路线才不会是一串格子中心的小碎步。
    const out = [];
    for (let i = 0; i < pts.length - 1;) {
      let j = pts.length - 1;
      while (j > i + 1 && !this.clear(pts[i], pts[j])) j--;
      out.push(pts[j]);
      i = j;
    }
    return out;
  }

  // 把点推回可行区域：先夹在墙内，再从家具里沿穿透最浅的一轴顶出来。
  clamp(p) {
    const { radius } = this;
    const xl = this.x0 + radius, xr = this.x0 + this.nx * this.cell - radius;
    const zl = this.z0 + radius, zr = this.z0 + this.nz * this.cell - radius;
    p.x = Math.min(xr, Math.max(xl, p.x));
    p.z = Math.min(zr, Math.max(zl, p.z));
    for (const o of this.obstacles) {
      const hw = o.hw + radius, hd = o.hd + radius;
      const dx = p.x - o.x, dz = p.z - o.z;
      if (Math.abs(dx) >= hw || Math.abs(dz) >= hd) continue;
      if (hw - Math.abs(dx) < hd - Math.abs(dz)) p.x = o.x + (dx < 0 ? -hw : hw);
      else p.z = o.z + (dz < 0 ? -hd : hd);
    }
  }
}
