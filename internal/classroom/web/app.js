// nanny's 3D school: each agent is a teacher with her class, each subagent a
// kid. Kids walk to the books, the computers, the lab or the globe by the tool
// they use; finished ones go to the park across the hallway and play on the
// swings, the slide, the seesaws, the merry-go-round, in the sandbox or at
// football, or play tag. Nobody walks through anything: they go round it.
// Around it, a little town.
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { CSS2DRenderer, CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';

const W = 14, D = 12, WALL = 2.8, GAP = 5;           // a room, and the gap between rooms
const COLS = 4, SEATS = 16;                            // desks
const DOOR_X = W / 2 - 1.6;                            // the door, front right
const HALL_Z = D / 2 + 1.5;                            // the hallway in front of the rooms
const PARK_MAX = 30;                                   // finished kids shown in the park
const PARK = { x0: -24, x1: 24, z0: 12, z1: 36 };      // the park, fenced
const ROAD_Z = 42;

// ---------- renderer, scene, camera ----------
const canvas = document.getElementById('scene');
const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, powerPreference: 'high-performance' });
renderer.setPixelRatio(Math.min(devicePixelRatio, 1.5));
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.outputColorSpace = THREE.SRGBColorSpace;
const labels = new CSS2DRenderer({ element: document.getElementById('labels') });

const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(45, 1, 0.1, 600);
camera.position.set(0, 34, 62);
const controls = new OrbitControls(camera, renderer.domElement);
controls.enableDamping = true;
controls.maxPolarAngle = Math.PI / 2.15;
controls.minDistance = 4;
controls.maxDistance = 140;
controls.target.set(0, 0.8, 14);
controls.autoRotateSpeed = 0.3;
let lastInput = performance.now();
for (const ev of ['pointerdown', 'wheel', 'keydown']) addEventListener(ev, () => { lastInput = performance.now(); controls.autoRotate = false; }, { passive: true });

function resize() {
  renderer.setSize(innerWidth, innerHeight, false);
  labels.setSize(innerWidth, innerHeight);
  camera.aspect = innerWidth / innerHeight;
  camera.updateProjectionMatrix();
}
addEventListener('resize', resize);
resize();

// ---------- materials and little helpers ----------
const mats = new Map();
function mat(color, o = {}) {
  const k = color + JSON.stringify(o);
  if (!mats.has(k)) mats.set(k, new THREE.MeshStandardMaterial({ color, roughness: 0.75, ...o }));
  return mats.get(k);
}
function shade(m) { m.castShadow = m.receiveShadow = true; return m; }
const geos = new Map(); // one geometry per shape and size, shared by every mesh
function geo(key, make) { if (!geos.has(key)) geos.set(key, make()); return geos.get(key); }
function box(w, h, d, color, o) { return shade(new THREE.Mesh(geo(`b${w},${h},${d}`, () => new THREE.BoxGeometry(w, h, d)), mat(color, o))); }
function cyl(rt, rb, h, color, seg = 14, o) { return shade(new THREE.Mesh(geo(`c${rt},${rb},${h},${seg}`, () => new THREE.CylinderGeometry(rt, rb, h, seg)), mat(color, o))); }
function sphere(r, color, o) { return shade(new THREE.Mesh(geo(`s${r}`, () => new THREE.SphereGeometry(r, 16, 12)), mat(color, o))); }
function capsule(r, len, color) { return shade(new THREE.Mesh(geo(`p${r},${len}`, () => new THREE.CapsuleGeometry(r, len, 4, 10)), mat(color))); }
// text sets a label's text only when it changes: the DOM is the slow part
function text(lbl, s) { if (lbl.element.textContent !== s) lbl.element.textContent = s; }
function at(obj, x, y, z) { obj.position.set(x, y, z); return obj; }
function hash(s) { let h = 2166136261; for (const c of String(s)) h = Math.imul(h ^ c.charCodeAt(0), 16777619); return h >>> 0; }
function pick(arr, s) { return arr[hash(s) % arr.length]; }
const rnd = (() => { let s = 7; return () => (s = (s * 16807) % 2147483647) / 2147483647; })(); // the same town every time
const lerp = (a, b, t) => a + (b - a) * t;
function lerpAngle(a, b, t) { let d = ((b - a + Math.PI) % (2 * Math.PI)) - Math.PI; if (d < -Math.PI) d += 2 * Math.PI; return a + d * t; }
function label(text, cls = 'label') { const d = document.createElement('div'); d.className = cls; d.textContent = text; return new CSS2DObject(d); }
let words = {};
const T = s => words[s] || s;
function ago(t) {
  const s = (Date.now() - new Date(t)) / 1000;
  if (!(s > 0)) return '';
  if (s < 60) return Math.round(s) + 's';
  if (s < 3600) return Math.round(s / 60) + 'm';
  if (s < 86400) return Math.round(s / 3600) + 'h';
  return Math.round(s / 86400) + 'd';
}
function short(s, n) { s = (s || '').replace(/\s+/g, ' ').trim(); return s.length > n ? s.slice(0, n - 1) + '…' : s; }
// instanced scatters many copies of one small mesh
function instanced(geo, material, list) {
  const m = new THREE.InstancedMesh(geo, material, list.length);
  const o = new THREE.Object3D(), c = new THREE.Color();
  list.forEach((it, i) => {
    o.position.set(it.x, it.y ?? 0, it.z); o.rotation.set(0, it.r ?? 0, 0); o.scale.setScalar(it.s ?? 1); o.updateMatrix();
    m.setMatrixAt(i, o.matrix);
    if (it.c != null) m.setColorAt(i, c.set(it.c));
  });
  m.castShadow = true; m.receiveShadow = true;
  scene.add(m);
  return m;
}

// ---------- sky and light by the hour ----------
const hour = () => { const d = new Date(); return d.getHours() + d.getMinutes() / 60; };
const night = () => { const h = hour(); return h < 7.5 || h >= 20; };
const skyMat = new THREE.ShaderMaterial({
  side: THREE.BackSide, depthWrite: false, fog: false,
  uniforms: { top: { value: new THREE.Color() }, bottom: { value: new THREE.Color() } },
  vertexShader: 'varying vec3 vP; void main(){ vP = normalize(position); gl_Position = projectionMatrix * modelViewMatrix * vec4(position,1.); }',
  fragmentShader: 'uniform vec3 top; uniform vec3 bottom; varying vec3 vP; void main(){ gl_FragColor = vec4(mix(bottom, top, smoothstep(-.05,.5,vP.y)), 1.); }',
});
scene.add(new THREE.Mesh(new THREE.SphereGeometry(450, 32, 16), skyMat));
const hemi = new THREE.HemisphereLight(0xffffff, 0x6b8f4e, 1.2);
scene.add(hemi);
const sun = new THREE.DirectionalLight(0xfff1d6, 2.4);
sun.castShadow = true;
sun.shadow.mapSize.set(2048, 2048);
sun.shadow.bias = -0.0004;
sun.shadow.normalBias = 0.03;
scene.add(sun, sun.target);
const starPos = [];
for (let i = 0; i < 1500; i++) { const u = rnd() * 2 * Math.PI, v = rnd() * 0.45 + 0.05, r = 420; starPos.push(r * Math.cos(u) * Math.cos(v), r * Math.sin(v), r * Math.sin(u) * Math.cos(v)); }
const stars = new THREE.Points(new THREE.BufferGeometry().setAttribute('position', new THREE.Float32BufferAttribute(starPos, 3)),
  new THREE.PointsMaterial({ color: 0xffffff, size: 1.3, sizeAttenuation: false, fog: false }));
scene.add(stars);
const glowing = []; // windows and lamps that light up at night
const winMat = new THREE.MeshStandardMaterial({ color: 0xbfe6ff, emissive: 0xffd27a });
const bulbMat = new THREE.MeshStandardMaterial({ color: 0xfff6d8, emissive: 0xffd27a });
const headMat = new THREE.MeshStandardMaterial({ color: 0xfff6c8, emissive: 0xfff2a0 });
function lightUp() {
  const n = night();
  skyMat.uniforms.top.value.set(n ? 0x0b1430 : 0x4aa3e8);
  skyMat.uniforms.bottom.value.set(n ? 0x2a2f5a : 0xcdeaf7);
  scene.fog = new THREE.Fog(n ? 0x1b2040 : 0xcdeaf7, 90, 320);
  hemi.intensity = n ? 0.35 : 1.2;
  sun.intensity = n ? 0.3 : 2.4;
  sun.color.set(n ? 0x9fb4ff : 0xfff1d6);
  stars.visible = n;
  document.body.style.background = n ? '#1b2040' : '#9fd3f5';
  for (const g of glowing) g.material.emissiveIntensity = n ? g.userData.on : g.userData.off;
}
function glow(mesh, on = 1.4, off = 0.05) { if (!glowing.some(g => g.material === mesh.material)) { mesh.userData.on = on; mesh.userData.off = off; glowing.push(mesh); } return mesh; }

// ---------- getting around: obstacles and paths ----------
// A Nav is a grid over a floor. Obstacles block it, each under a tag, and a
// walker may pass through its own (its chair, the game it plays). Paths are
// A* over the grid, then pulled straight wherever nothing is in the way.
const EDGE = 1 << 30; // outside the floor: a wall or a fence
class Nav {
  constructor(x0, z0, x1, z1, cell, r) {
    Object.assign(this, { x0, z0, x1, z1, cell, r, obs: [], tags: new Map(), grid: null });
    this.nx = Math.ceil((x1 - x0) / cell); this.nz = Math.ceil((z1 - z0) / cell);
  }
  bit(tag) { if (!this.tags.has(tag)) this.tags.set(tag, 1 << this.tags.size); return this.tags.get(tag); }
  circle(tag, x, z, r) { this.obs.push({ b: this.bit(tag), x, z, r }); this.grid = null; }
  box(tag, x0, z0, x1, z1) { this.obs.push({ b: this.bit(tag), x0, z0, x1, z1 }); this.grid = null; }
  // hit is the tags of what a walker standing at x, z would touch
  hit(x, z, r = this.r) {
    let m = x < this.x0 + r || x > this.x1 - r || z < this.z0 + r || z > this.z1 - r ? EDGE : 0;
    for (const o of this.obs) {
      if (o.r !== undefined ? (x - o.x) ** 2 + (z - o.z) ** 2 < (o.r + r) ** 2
        : x > o.x0 - r && x < o.x1 + r && z > o.z0 - r && z < o.z1 + r) m |= o.b;
    }
    return m;
  }
  cx(i) { return this.x0 + (i % this.nx + 0.5) * this.cell; }
  cz(i) { return this.z0 + (Math.floor(i / this.nx) + 0.5) * this.cell; }
  // near is the free cell closest to p
  near(p, free) {
    const ix = Math.max(0, Math.min(this.nx - 1, Math.floor((p.x - this.x0) / this.cell)));
    const iz = Math.max(0, Math.min(this.nz - 1, Math.floor((p.z - this.z0) / this.cell)));
    let best = -1, bd = Infinity;
    for (let r = 0; r < 24 && best < 0; r++) for (let dz = -r; dz <= r; dz++) for (let dx = -r; dx <= r; dx++) {
      if (Math.max(Math.abs(dx), Math.abs(dz)) !== r) continue;
      const x = ix + dx, z = iz + dz, i = z * this.nx + x;
      if (x < 0 || z < 0 || x >= this.nx || z >= this.nz || !free(i)) continue;
      const d = (this.cx(i) - p.x) ** 2 + (this.cz(i) - p.z) ** 2;
      if (d < bd) { bd = d; best = i; }
    }
    return best;
  }
  // clear says a walker can go straight from a to b
  clear(a, b, skip) {
    const n = Math.ceil(Math.hypot(b.x - a.x, b.z - a.z) / 0.08);
    for (let i = 1; i < n; i++) if (this.hit(lerp(a.x, b.x, i / n), lerp(a.z, b.z, i / n)) & ~skip) return false;
    return true;
  }
  // path is the way from `from` to `to`, its corners only, ending at `to`
  path(from, to, skip = 0) {
    if (!this.grid) {
      this.grid = new Int32Array(this.nx * this.nz);
      for (let i = 0; i < this.grid.length; i++) this.grid[i] = this.hit(this.cx(i), this.cz(i));
    }
    const free = i => !(this.grid[i] & ~skip);
    const end = to.clone(), y = to.y;
    if (this.clear(from, to, skip) && !(this.hit(to.x, to.z) & ~skip)) return [end];
    const s = this.near(from, free), g = this.near(to, free);
    if (s < 0 || g < 0) return [end];
    const N = this.grid.length, cost = new Float32Array(N).fill(Infinity), came = new Int32Array(N).fill(-1), done = new Uint8Array(N);
    const hx = this.cx(g), hz = this.cz(g), h = i => Math.hypot(this.cx(i) - hx, this.cz(i) - hz);
    const heap = [[h(s), s]];
    const push = e => { heap.push(e); let i = heap.length - 1; while (i && heap[(i - 1) >> 1][0] > heap[i][0]) { [heap[i], heap[(i - 1) >> 1]] = [heap[(i - 1) >> 1], heap[i]]; i = (i - 1) >> 1; } };
    const pop = () => {
      const top = heap[0], last = heap.pop();
      if (heap.length) {
        heap[0] = last;
        for (let i = 0; ;) {
          const l = 2 * i + 1, r = l + 1; let m = i;
          if (l < heap.length && heap[l][0] < heap[m][0]) m = l;
          if (r < heap.length && heap[r][0] < heap[m][0]) m = r;
          if (m === i) break;
          [heap[i], heap[m]] = [heap[m], heap[i]]; i = m;
        }
      }
      return top;
    };
    cost[s] = 0;
    while (heap.length) {
      const [, i] = pop();
      if (i === g) break;
      if (done[i]) continue;
      done[i] = 1;
      const x = i % this.nx, z = Math.floor(i / this.nx);
      for (let dz = -1; dz <= 1; dz++) for (let dx = -1; dx <= 1; dx++) {
        if (!dx && !dz) continue;
        const nx = x + dx, nz = z + dz;
        if (nx < 0 || nz < 0 || nx >= this.nx || nz >= this.nz) continue;
        const j = nz * this.nx + nx;
        if (!free(j) || dx && dz && (!free(z * this.nx + nx) || !free(nz * this.nx + x))) continue; // no cutting corners
        const c = cost[i] + (dx && dz ? Math.SQRT2 : 1);
        if (c < cost[j]) { cost[j] = c; came[j] = i; push([c + h(j), j]); }
      }
    }
    if (g !== s && came[g] < 0) return [end];
    const pts = [from.clone()];
    const cells = [];
    for (let i = g; i !== -1 && i !== s; i = came[i]) cells.push(i);
    cells.push(s);
    for (const i of cells.reverse()) pts.push(new THREE.Vector3(this.cx(i), y, this.cz(i)));
    if (!(this.hit(to.x, to.z) & ~skip)) pts.push(end);
    const out = [];
    for (let i = 0; i < pts.length - 1;) { // pulled straight
      let j = i + 1;
      while (j < pts.length - 1 && this.clear(pts[i], pts[j + 1], skip)) j++;
      out.push(pts[j]);
      i = j;
    }
    return out;
  }
}

// ---------- what stands where ----------
// Every solid thing in the town and the park gets its box here, on the ground
// and in height, and nothing new goes where something already stands.
const solids = [], misplaced = [];
let owner = 0;
function boxAt(name, x, z, w, d, h, y0 = 0) { return { name, x0: x - w / 2, z0: z - d / 2, x1: x + w / 2, z1: z + d / 2, y0, y1: y0 + h }; }
function overlaps(a, b) { return a.own !== b.own && a.x0 < b.x1 && b.x0 < a.x1 && a.z0 < b.z1 && b.z0 < a.z1 && a.y0 < b.y1 && b.y0 < a.y1; }
function fits(...bs) { return bs.every(b => !solids.some(s => overlaps(b, s))); }
// place notes the boxes of one thing if they are free; must says it has to fit
function place(bs, must) {
  const own = ++owner;
  bs.forEach(b => { b.own = own; });
  if (!fits(...bs)) { if (must) misplaced.push(bs[0].name); else return false; }
  solids.push(...bs);
  return true;
}
// flat ground nothing is planted on: the road and its sidewalks, the park,
// the school grounds (rooms come and go there)
const flats = [
  { x0: -450, z0: ROAD_Z - 5.5, x1: 450, z1: ROAD_Z + 5.5 },
  { x0: PARK.x0 - 0.6, z0: PARK.z0 - 0.6, x1: PARK.x1 + 0.6, z1: PARK.z1 + 0.6 },
  { x0: -450, z0: -16, x1: 450, z1: PARK.z0 - 0.5 },
];
const onFlat = (x, z) => flats.some(f => x > f.x0 && x < f.x1 && z > f.z0 && z < f.z1);

// ---------- the town: grass, roads, houses, trees, clouds ----------
{
  const g = new THREE.PlaneGeometry(900, 900, 90, 90);
  const cols = [], c = new THREE.Color();
  for (let i = 0; i < g.attributes.position.count; i++) { c.setHSL(0.27 + rnd() * 0.04, 0.45, 0.42 + rnd() * 0.08); cols.push(c.r, c.g, c.b); }
  g.setAttribute('color', new THREE.Float32BufferAttribute(cols, 3));
  const ground = new THREE.Mesh(g, new THREE.MeshStandardMaterial({ vertexColors: true, roughness: 1 }));
  ground.rotation.x = -Math.PI / 2;
  ground.receiveShadow = true;
  scene.add(ground);
}
const hall = box(1, 0.06, 3, 0xd9d2c3);
scene.add(hall);
const plaza = box(1, 0.04, PARK.z0 - HALL_Z - 1.5, 0xcfc6b4); // between the school and the park
scene.add(plaza);
// the road in front of the park, its sidewalks and its lines
scene.add(at(box(900, 0.05, 7, 0x3b3f46), 0, 0.03, ROAD_Z));
for (const z of [ROAD_Z - 4.5, ROAD_Z + 4.5]) scene.add(at(box(900, 0.15, 2, 0xc9c2b4), 0, 0.075, z));
instanced(new THREE.BoxGeometry(2.4, 0.02, 0.2), mat(0xf4f1e6), Array.from({ length: 120 }, (_, i) => ({ x: -300 + i * 5, y: 0.06, z: ROAD_Z })));
// cars driving both ways, headlights on at night; every car in a lane goes
// at the lane's speed, so none drives into the one ahead
const cars = [];
function car(color, x, dir) {
  const g = new THREE.Group();
  g.add(at(box(3.4, 0.7, 1.6, color), 0, 0.6, 0));
  g.add(at(box(1.9, 0.6, 1.4, color), -0.2, 1.2, 0));
  const glass = mat(0x9ad0ff, { roughness: 0.1, metalness: 0.3 });
  const w1 = at(new THREE.Mesh(new THREE.BoxGeometry(1.7, 0.45, 1.42), glass), -0.2, 1.22, 0);
  g.add(w1);
  for (const [wx, wz] of [[-1.1, 0.8], [1.1, 0.8], [-1.1, -0.8], [1.1, -0.8]]) {
    const w = cyl(0.33, 0.33, 0.25, 0x1a1a1a, 16); w.rotation.x = Math.PI / 2; g.add(at(w, wx, 0.33, wz));
  }
  for (const s of [-0.5, 0.5]) g.add(at(glow(new THREE.Mesh(geo('hl', () => new THREE.BoxGeometry(0.08, 0.18, 0.3)), headMat), 2.5, 0.2), 1.72, 0.65, s));
  g.rotation.y = dir > 0 ? 0 : Math.PI;
  at(g, x, 0, ROAD_Z + (dir > 0 ? 1.7 : -1.7));
  scene.add(g);
  cars.push({ g, dir, speed: dir > 0 ? 8 : 6.5 });
}
[[0xe5484d, -60, 1], [0x3e8ef7, 20, 1], [0xf5a524, 70, -1], [0x30a46c, -10, -1], [0xffffff, 110, 1]].forEach(a => car(...a));
// houses across the road and behind the school
function house(x, z, rot, seed) {
  const g = new THREE.Group();
  const wall = pick([0xf3d9b1, 0xe9c4c4, 0xc9dbe9, 0xf6efe2, 0xd8e8c8, 0xe8d2f0], seed + 'w');
  const roof = pick([0xb5482e, 0x6b4a2b, 0x3e4c6b, 0x8e3b2f], seed + 'r');
  const w = 6 + (hash(seed) % 3), h = 3.2 + (hash(seed + 'h') % 2) * 2.4;
  g.add(at(box(w, h, 5, wall), 0, h / 2, 0));
  const r = shade(new THREE.Mesh(new THREE.ConeGeometry(w * 0.78, 2.4, 4), mat(roof, { flatShading: true })));
  r.rotation.y = Math.PI / 4; r.scale.z = 5 / w;
  g.add(at(r, 0, h + 1.2, 0));
  g.add(at(box(1, 1.9, 0.1, 0x6b4a2b), 0, 0.95, 2.52));
  for (let fl = 0; fl < Math.round(h / 2.4); fl++) for (const wx of [-w / 3, w / 3]) {
    g.add(at(glow(new THREE.Mesh(geo('win', () => new THREE.BoxGeometry(1, 0.9, 0.08)), winMat), 1.2, 0.0), wx, 1.6 + fl * 2.4, 2.52));
  }
  g.add(at(box(0.5, 1.6, 0.5, 0x8a8a8a), w / 3, h + 1.2, -1));                 // a chimney
  g.rotation.y = rot;
  scene.add(at(g, x, 0, z));
  place([boxAt('house', x, z, w * 1.1, 5.6, h + 2.4)], true);
}
for (let i = 0; i < 16; i++) house(-90 + i * 12 + (hash('hx' + i) % 3), ROAD_Z + 13, Math.PI, 'f' + i);
for (let i = 0; i < 14; i++) house(-80 + i * 12.5, -30 - (hash('bz' + i) % 6), 0, 'b' + i);
// trees are instanced: a few draw calls for all of them, built by plantTrees;
// a tree goes only where its trunk and its crown are free
const treeSpecs = [];
function tree(x, z, s = 1, kind = 0, must = false) {
  if (!place([boxAt('trunk', x, z, 0.55 * s, 0.55 * s, 1.8 * s), boxAt('crown', x, z, 3.3 * s, 3.3 * s, 3.3 * s, 1.15 * s)], must)) return false;
  treeSpecs.push({ x, z, s, kind });
  return true;
}
function plantTrees() {
  const trunks = [], leaves = [], cones = [];
  for (const { x, z, s, kind } of treeSpecs) {
    trunks.push({ x, z, y: 0.9 * s, s });
    if (kind % 3 === 2) for (let i = 0; i < 3; i++) cones.push({ x, z, y: (2 + i * 0.9) * s, s: s * (1.4 - i * 0.35) / 1.4 });
    else for (const [dx, dy, dz, r] of [[0, 2.4, 0, 1.2], [0.7, 2.1, 0.3, 0.85], [-0.6, 2.2, -0.3, 0.9], [0, 3, 0, 0.8]]) {
      leaves.push({ x: x + dx * s, z: z + dz * s, y: dy * s, s: r * s, c: kind % 3 ? 0x6aa84f : 0x4f9a3f });
    }
  }
  instanced(new THREE.CylinderGeometry(0.18, 0.25, 1.8, 8), mat(0x8a5a33), trunks);
  instanced(new THREE.IcosahedronGeometry(1, 1), new THREE.MeshStandardMaterial({ roughness: 0.8, flatShading: true }), leaves);
  instanced(new THREE.ConeGeometry(1.4, 1.6, 8), mat(0x2f6b3a, { flatShading: true }), cones);
}
for (let n = 0, i = 0; n < 70 && i < 800; i++) if (tree((rnd() - 0.5) * 220, -22 - rnd() * 30, 0.9 + rnd() * 0.7, i)) n++;
for (let i = 0; i < 30; i++) tree(-100 + i * 7 + rnd() * 1.5, ROAD_Z + 6.5, 0.8 + rnd() * 0.3, i + 1);
for (const [x, z] of [[-27, 14], [-27, 24], [-27, 33.6], [27, 14], [27, 25], [27, 33.6]]) tree(x, z, 1.2, hash(x + ',' + z), true);
const clouds = [];
for (let i = 0; i < 14; i++) {
  const g = new THREE.Group();
  const cm = new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 1, transparent: true, opacity: 0.92 });
  for (let j = 0; j < 5; j++) g.add(at(new THREE.Mesh(new THREE.SphereGeometry(2 + rnd() * 2, 12, 8), cm), j * 2.4 - 5, rnd() * 1.2, rnd() * 2));
  at(g, (rnd() - 0.5) * 300, 38 + rnd() * 18, -80 + rnd() * 160);
  scene.add(g);
  clouds.push(g);
}
// street lamps along the park's sidewalk
const lamps = [];
function lamp(x, z) {
  scene.add(at(cyl(0.07, 0.1, 3.4, 0x2f3437, 8), x, 1.7, z));
  const bulb = glow(new THREE.Mesh(geo('bulb', () => new THREE.SphereGeometry(0.22, 12, 8)), bulbMat), 3, 0.1);
  scene.add(at(bulb, x, 3.5, z));
  lamps.push(bulb);
  place([boxAt('lamp', x, z, 0.44, 0.44, 3.75)], true);
}
for (const x of [-30, 0, 30]) lamp(x, ROAD_Z - 5);

// ---------- the park ----------
const park = new THREE.Group();
scene.add(park);
const parkNav = new Nav(PARK.x0 + 0.1, PARK.z0 + 0.1, PARK.x1 - 0.1, PARK.z1 - 0.1, 0.3, 0.3);
// prop notes a thing in the park: it stands there, so nothing else does, and
// walkers go round it unless it is their own game
function prop(tag, x0, z0, x1, z1, h, y0 = 0) {
  place([{ name: tag, x0, z0, x1, z1, y0, y1: y0 + h }], true);
  parkNav.box(tag, x0, z0, x1, z1);
}
function propC(tag, x, z, r, h) {
  place([boxAt(tag, x, z, 2 * r, 2 * r, h)], true);
  parkNav.circle(tag, x, z, r);
}
park.add(at(box(PARK.x1 - PARK.x0, 0.04, PARK.z1 - PARK.z0, 0x86c06a), (PARK.x0 + PARK.x1) / 2, 0.02, (PARK.z0 + PARK.z1) / 2));
park.add(at(box(2.4, 0.06, 10, 0xd8c8a8), 0, 0.05, PARK.z0 + 5));            // the path from the gate to the fountain
{ // the fence, with a gate at the middle of the near side
  const posts = [], fence = [];
  for (let x = PARK.x0; x <= PARK.x1; x += 1.5) for (const z of [PARK.z0, PARK.z1]) if (z !== PARK.z0 || Math.abs(x) > 1.6) posts.push({ x, z, y: 0.5 });
  for (let z = PARK.z0; z <= PARK.z1; z += 1.5) for (const x of [PARK.x0, PARK.x1]) posts.push({ x, z, y: 0.5 });
  instanced(new THREE.BoxGeometry(0.14, 1, 0.14), mat(0xffffff), posts);
  for (const [x, z, w, d] of [[(PARK.x0 - 1.6) / 2, PARK.z0, -PARK.x0 - 1.6, 0.08], [(PARK.x1 + 1.6) / 2, PARK.z0, PARK.x1 - 1.6, 0.08],
    [0, PARK.z1, PARK.x1 - PARK.x0, 0.08], [PARK.x0, (PARK.z0 + PARK.z1) / 2, 0.08, PARK.z1 - PARK.z0], [PARK.x1, (PARK.z0 + PARK.z1) / 2, 0.08, PARK.z1 - PARK.z0]]) {
    for (const y of [0.35, 0.75]) park.add(at(box(w, 0.08, d, 0xffffff), x, y, z));
    fence.push(boxAt('fence', x, z, Math.max(w, 0.14), Math.max(d, 0.14), 1));
  }
  for (const s of [-1, 1]) { park.add(at(box(0.25, 2.2, 0.25, 0x8a5a33), s * 1.6, 1.1, PARK.z0)); fence.push(boxAt('gate', s * 1.6, PARK.z0, 0.25, 0.25, 2.6)); }
  place(fence, true);
  const arch = at(box(3.5, 0.5, 0.3, 0x8a5a33), 0, 2.35, PARK.z0);
  park.add(arch);
}
const GATE = new THREE.Vector3(0, 0.06, PARK.z0), GATE_IN = new THREE.Vector3(0, 0.06, PARK.z0 + 0.9);
const parkSign = label('', 'sign');
park.add(at(parkSign, 0, 3.1, PARK.z0));
// swings: four seats hanging from a frame, each in its own lane
const SWING = { x: -17, z: 18, n: 4 };
{
  const f = new THREE.Group();
  for (const s of [-1, 1]) for (const d of [-0.9, 0.9]) {
    const leg = box(0.12, 3, 0.12, 0xe5484d); leg.rotation.x = d * 0.2; f.add(at(leg, s * 3, 1.45, d * 0.3));
  }
  f.add(at(box(6.2, 0.14, 0.14, 0xe5484d), 0, 2.9, 0));
  park.add(at(f, SWING.x, 0, SWING.z));
  place([boxAt('swings', SWING.x, SWING.z, 6.4, 3.3, 3)], true);
  for (const s of [-1, 1]) for (const d of [-1, 1]) parkNav.circle('swingframe', SWING.x + s * 3.05, SWING.z + d * 0.55, 0.12);
}
const swings = [];
for (let i = 0; i < SWING.n; i++) {
  const pivot = new THREE.Group();
  for (const s of [-0.22, 0.22]) pivot.add(at(box(0.03, 2, 0.03, 0x666666), s, -1, 0));
  pivot.add(at(box(0.55, 0.06, 0.3, 0x3e8ef7), 0, -2, 0));
  park.add(at(pivot, SWING.x - 2.25 + i * 1.5, 2.85, SWING.z));
  parkNav.box('swing' + i, pivot.position.x - 0.45, SWING.z - 1.6, pivot.position.x + 0.45, SWING.z + 1.6);
  swings.push(pivot);
}
// the slide: a ladder up to a platform, a ramp down toward the fountain
const SLIDE = { x: -6, z: 16 };
{
  const g = new THREE.Group();
  for (const [x, z] of [[-0.6, -0.6], [0.6, -0.6], [-0.6, 0.6], [0.6, 0.6]]) g.add(at(box(0.12, 2.2, 0.12, 0xf5a524), x, 1.1, z));
  g.add(at(box(1.4, 0.12, 1.4, 0x3e8ef7), 0, 2.2, 0));
  for (let i = 0; i < 6; i++) g.add(at(box(1.1, 0.06, 0.08, 0xcccccc), 0, 0.35 * (i + 1), -1.1 - i * 0.05));
  for (const s of [-0.55, 0.55]) { const r = box(0.08, 2.5, 0.08, 0xcccccc); r.rotation.x = -0.15; g.add(at(r, s, 1.2, -1.2)); }
  const ramp = box(1.1, 0.08, 4.6, 0xe5484d); ramp.rotation.x = 0.45; g.add(at(ramp, 0, 1.15, 2.7));
  for (const s of [-0.55, 0.55]) { const side = box(0.08, 0.3, 4.6, 0xf5a524); side.rotation.x = 0.45; g.add(at(side, s, 1.3, 2.7)); }
  park.add(at(g, SLIDE.x, 0, SLIDE.z));
  prop('slide', SLIDE.x - 0.7, SLIDE.z - 1.45, SLIDE.x + 0.7, SLIDE.z + 4.85, 2.6);
}
// two seesaws
const SEESAW = [{ x: 4, z: 16 }, { x: 4, z: 19.5 }];
const planks = SEESAW.map((s, j) => {
  park.add(at(box(0.4, 0.5, 0.4, 0x8e4ec6), s.x, 0.25, s.z));
  const p = new THREE.Group();
  p.add(at(box(4, 0.1, 0.35, 0x30a46c), 0, 0, 0));
  for (const e of [-1.75, 1.75]) p.add(at(box(0.06, 0.4, 0.3, 0x333333), e, 0.25, 0));
  park.add(at(p, s.x, 0.55, s.z));
  prop('seesaw' + j, s.x - 2.05, s.z - 0.25, s.x + 2.05, s.z + 0.25, 1.4);
  return p;
});
// the merry-go-round
const MERRY = { x: 15, z: 17, r: 1.5 };
const merry = new THREE.Group();
merry.add(at(cyl(1.9, 1.9, 0.15, 0x0091ff, 28), 0, 0.3, 0));
for (let i = 0; i < 4; i++) { const a = i * Math.PI / 2; merry.add(at(box(0.06, 1, 0.06, 0xf5a524), Math.cos(a) * 1.1, 0.85, Math.sin(a) * 1.1)); }
merry.add(at(cyl(0.12, 0.12, 1.4, 0xf5a524), 0, 0.9, 0));
park.add(at(merry, MERRY.x, 0, MERRY.z));
propC('merry', MERRY.x, MERRY.z, 1.95, 1.6);
// the fountain in the middle
const FOUNT = new THREE.Vector3(0, 0, 23);
const water = new THREE.Mesh(new THREE.CylinderGeometry(2.1, 2.1, 0.1, 28), new THREE.MeshStandardMaterial({ color: 0x5ab4ff, roughness: 0.1, transparent: true, opacity: 0.85 }));
{
  park.add(at(cyl(2.5, 2.6, 0.6, 0xc9c2b4, 28), FOUNT.x, 0.3, FOUNT.z));
  park.add(at(water, FOUNT.x, 0.62, FOUNT.z));
  park.add(at(cyl(0.25, 0.35, 1.4, 0xc9c2b4), FOUNT.x, 1, FOUNT.z));
  propC('fountain', FOUNT.x, FOUNT.z, 2.6, 3);
}
const jet = new THREE.Mesh(new THREE.ConeGeometry(0.35, 1.4, 12, 1, true), new THREE.MeshStandardMaterial({ color: 0xbfe6ff, transparent: true, opacity: 0.6 }));
park.add(at(jet, FOUNT.x, 2.3, FOUNT.z));
// the sandbox, a castle in the middle and a bucket in a corner
const SAND = { x: -16, z: 28 };
park.add(at(box(5, 0.25, 5, 0xe9d8a6), SAND.x, 0.12, SAND.z));
for (const [w, d, x, z] of [[5.3, 0.25, 0, -2.6], [5.3, 0.25, 0, 2.6], [0.25, 5, -2.6, 0], [0.25, 5, 2.6, 0]]) park.add(at(box(w, 0.35, d, 0x8a5a33), SAND.x + x, 0.17, SAND.z + z));
park.add(at(cyl(0.6, 0.8, 0.6, 0xdcc48e, 4), SAND.x, 0.55, SAND.z));
park.add(at(cyl(0.12, 0.1, 0.25, 0xe5484d), SAND.x - 2, 0.4, SAND.z - 2));
prop('sand', SAND.x - 2.75, SAND.z - 2.75, SAND.x + 2.75, SAND.z + 2.75, 0.35);
parkNav.circle('toys', SAND.x, SAND.z, 0.8);
parkNav.circle('toys', SAND.x - 2, SAND.z - 2, 0.15);
// the football pitch: lines, two goals, the ball
const PITCH = { x: 5, z: 29, w: 12, d: 7 };
park.add(at(box(PITCH.w, 0.03, PITCH.d, 0x6fb35a), PITCH.x, 0.05, PITCH.z));
for (const [w, d, x, z] of [[PITCH.w, 0.1, 0, -PITCH.d / 2], [PITCH.w, 0.1, 0, PITCH.d / 2], [0.1, PITCH.d, -PITCH.w / 2, 0], [0.1, PITCH.d, PITCH.w / 2, 0], [0.1, PITCH.d, 0, 0]]) park.add(at(box(w, 0.02, d, 0xffffff), PITCH.x + x, 0.07, PITCH.z + z));
parkNav.box('pitch', PITCH.x - PITCH.w / 2, PITCH.z - PITCH.d / 2, PITCH.x + PITCH.w / 2, PITCH.z + PITCH.d / 2);
for (const s of [-1, 1]) {
  const goal = new THREE.Group();
  for (const z of [-1.2, 1.2]) goal.add(at(box(0.1, 1.2, 0.1, 0xffffff), 0, 0.6, z));
  goal.add(at(box(0.1, 0.1, 2.5, 0xffffff), 0, 1.2, 0));
  park.add(at(goal, PITCH.x + s * PITCH.w / 2, 0, PITCH.z));
  prop('goal', PITCH.x + s * PITCH.w / 2 - 0.06, PITCH.z - 1.26, PITCH.x + s * PITCH.w / 2 + 0.06, PITCH.z + 1.26, 1.25);
}
const ball = sphere(0.2, 0xffffff);
ball.material = new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 0.4 });
park.add(at(ball, PITCH.x, 0.2, PITCH.z));
const game = { from: new THREE.Vector3(PITCH.x, 0.2, PITCH.z), to: new THREE.Vector3(PITCH.x, 0.2, PITCH.z), t: 1, holder: null };
// where the ones who don't fit in a game watch the football, and the lawn
// where they play tag
const WATCH = { x: PITCH.x - 4.5, z: PITCH.z + PITCH.d / 2 + 1.1 };
const TAG = { x0: 14.3, z0: 23.3, x1: 21.6, z1: 33.2 };
// benches and lamps along the paths
for (const [x, z, r] of [[-3, 15.2, Math.PI / 2], [8.5, 22.5, 0], [-9.5, 23.5, 0], [4, 24.6, Math.PI], [-9, 31, 0], [12, 22, Math.PI / 2]]) {
  const b = new THREE.Group();
  b.add(at(box(1.8, 0.08, 0.5, 0x8a5a33), 0, 0.5, 0));
  b.add(at(box(1.8, 0.45, 0.08, 0x8a5a33), 0, 0.8, -0.25));
  for (const s of [-0.75, 0.75]) b.add(at(box(0.08, 0.5, 0.45, 0x333333), s, 0.25, 0));
  b.rotation.y = r;
  park.add(at(b, x, 0, z));
  const side = Math.abs(Math.sin(r)) > 0.5;
  prop('bench', x - (side ? 0.3 : 0.92), z - (side ? 0.92 : 0.3), x + (side ? 0.3 : 0.92), z + (side ? 0.92 : 0.3), 1.05);
}
for (const [x, z] of [[-2, PARK.z0 + 1], [2, PARK.z0 + 1], [-12, 22], [12, 28], [-20, 30], [10, 14.5]]) {
  lamp(x, z);
  parkNav.circle('lamp', x, z, 0.14);
}
// trees and bushes in the park's corners
for (const [x, z, s] of [[-22.3, 33.6, 1.2], [22.3, 14.2, 1.2], [-9.5, 34.2, 1.1]]) { tree(x, z, s, hash(x + 't'), true); parkNav.circle('tree', x, z, 0.3 * s); }
{
  const bushes = [];
  for (const [x, z, s] of [[-23, 35.1, 1], [-21.6, 35.2, 0.9], [23, 35.1, 1], [21.6, 35.2, 0.9], [-23, 12.9, 0.9], [23, 12.9, 0.9]]) {
    if (!place([boxAt('bush', x, z, 1.4 * s, 1.4 * s, 1.1 * s)])) { misplaced.push('bush'); continue; }
    bushes.push({ x, z, y: 0.4, s });
    parkNav.circle('bush', x, z, 0.7 * s);
  }
  instanced(new THREE.IcosahedronGeometry(0.7, 1), mat(0x3f8f3a, { flatShading: true }), bushes);
}
// flowers on the grass, never under anything nor on the road or in the park
{
  const flowers = [];
  for (let i = 0; i < 2000 && flowers.length < 700; i++) {
    const x = (rnd() - 0.5) * 160, z = -20 + rnd() * 72;
    if (onFlat(x, z) || !fits(boxAt('flower', x, z, 0.4, 0.4, 0.3))) continue;
    flowers.push({ x, z, y: 0.12, s: 0.6 + rnd() * 0.8, c: pick([0xff6b8b, 0xffd23f, 0xffffff, 0xb07cff, 0xff9f43], i) });
  }
  instanced(new THREE.IcosahedronGeometry(0.12, 0), new THREE.MeshStandardMaterial({ roughness: 0.8 }), flowers);
}

plantTrees();
// nothing in the town or the park moves but these: skip their matrix updates
const moving = new Set([...swings, ...planks, merry, water, jet, ball, ...cars.map(c => c.g), ...clouds, hall, plaza, sun, sun.target]);
scene.traverse(o => { if (!moving.has(o) && !(o.parent && moving.has(o.parent))) { o.updateMatrix(); o.matrixAutoUpdate = false; } });
for (const o of moving) o.traverse(ch => { ch.matrixAutoUpdate = true; });

// ---------- people ----------
const SKIN = [0xf1c7a5, 0xe0ac8a, 0xc68863, 0x8d5a3b, 0xf6d7bf];
const HAIR = [0x2b1d14, 0x5a3a22, 0xa8682f, 0xe3c16f, 0x1a1a1a, 0xb5482e];
const SHIRT = { Explore: 0x3e8ef7, Plan: 0x9b6dff, 'general-purpose': 0xf5a524, 'claude-code-guide': 0x30a46c };
function shirtOf(type) { return SHIRT[type] ?? pick([0xe5484d, 0x12a594, 0xd6409f, 0x8e4ec6, 0x0091ff, 0xf76b15], type || 'x'); }

// person builds a body facing +z; kids are small with big heads and a cap in
// the colour of their class.
function person({ seed, shirt, pants = 0x34405a, teacher = false, cap }) {
  const root = new THREE.Group(), body = new THREE.Group();
  root.add(body);
  const skin = pick(SKIN, seed + 's'), hair = pick(HAIR, seed + 'h');
  const leg = side => {
    const p = at(new THREE.Group(), side * 0.11, 0.8, 0);
    p.add(at(capsule(0.075, 0.6, teacher ? skin : pants), 0, -0.4, 0));
    p.add(at(box(0.16, 0.09, 0.26, 0x2a2a2a), 0, -0.78, 0.05));
    body.add(p);
    return p;
  };
  const arm = side => {
    const p = at(new THREE.Group(), side * 0.29, 1.36, 0);
    p.add(at(capsule(0.065, 0.42, shirt), 0, -0.25, 0));
    const hand = at(new THREE.Group(), 0, -0.52, 0);
    hand.add(sphere(0.07, skin));
    p.add(hand);
    body.add(p);
    p.userData.hand = hand;
    return p;
  };
  const legL = leg(-1), legR = leg(1);
  const torso = capsule(0.23, 0.42, shirt);
  torso.scale.z = 0.7;
  body.add(at(torso, 0, 1.12, 0));
  if (teacher) {
    const skirt = shade(new THREE.Mesh(new THREE.ConeGeometry(0.36, 0.7, 20, 1, true), mat(0x2f3e66, { side: THREE.DoubleSide })));
    body.add(at(skirt, 0, 0.72, 0));
  }
  const armL = arm(-1), armR = arm(1);
  const head = at(new THREE.Group(), 0, 1.66, 0);
  head.add(sphere(0.23, skin));
  const hairCap = new THREE.Mesh(new THREE.SphereGeometry(0.245, 20, 12, 0, Math.PI * 2, 0, Math.PI * 0.55), mat(hair));
  hairCap.rotation.x = -0.25;
  head.add(hairCap);
  if (teacher) {
    head.add(at(sphere(0.12, hair), 0, 0.12, -0.2)); // a bun
    for (const s of [-1, 1]) head.add(at(new THREE.Mesh(new THREE.TorusGeometry(0.055, 0.012, 6, 16), mat(0x222222)), s * 0.085, 0.02, 0.215));
  } else {
    if (hash(seed) % 2) for (const s of [-1, 1]) head.add(at(sphere(0.07, hair), s * 0.22, 0.05, -0.05)); // pigtails
    if (cap != null) {
      const c = new THREE.Mesh(new THREE.SphereGeometry(0.255, 20, 10, 0, Math.PI * 2, 0, Math.PI * 0.45), mat(cap));
      c.rotation.x = -0.15;
      head.add(c);
      head.add(at(box(0.3, 0.03, 0.22, cap), 0, 0.08, 0.27));
    }
  }
  for (const s of [-1, 1]) {
    head.add(at(sphere(0.03, 0x1a1a1a), s * 0.085, 0.02, 0.21));
    if (!teacher) head.add(at(sphere(0.035, 0xff9aa2), s * 0.13, -0.06, 0.18));
  }
  head.add(at(box(0.08, 0.018, 0.02, 0x7a2a2a), 0, -0.09, 0.215));
  body.add(head);
  if (!teacher) { root.scale.setScalar(0.66); head.scale.setScalar(1.25); }
  root.traverse(o => { if (o.isMesh) o.userData.actor = root; });
  root.userData.parts = { body, legL, legR, armL, armR, head };
  root.userData.sitY = teacher ? -0.36 : -0.06; // hips on a chair
  return root;
}

// pose eases a body toward a pose: walk, run, sit, think, hand, work, write,
// wave, cheer, desk, sip, eat, swing, ride, hold, dig, kick, climb, slide, idle.
function pose(p, kind, t, k = 1) {
  const { body, legL, legR, armL, armR, head } = p.userData.parts;
  const sy = p.userData.sitY;
  let lx = 0, rx = 0, al = 0, ar = 0, alz = 0, arz = 0, by = 0, brx = 0, hy = 0, hx = 0;
  const seated = () => { lx = rx = -Math.PI / 2; by = sy; };
  switch (kind) {
    case 'walk': { const s = Math.sin(t * 9 * k); lx = s * 0.7; rx = -s * 0.7; al = -s * 0.6; ar = s * 0.6; by = Math.abs(Math.cos(t * 9 * k)) * 0.05; break; }
    case 'run': { const s = Math.sin(t * 13); lx = s; rx = -s; al = -s; ar = s; by = Math.abs(Math.cos(t * 13)) * 0.12; break; }
    case 'sit': seated(); al = ar = -0.9; hx = 0.15 + Math.sin(t * 2) * 0.03; break;
    case 'think': seated(); al = -0.3; ar = -2.4; arz = 0.5; hx = -0.2; hy = Math.sin(t * 0.8) * 0.3; break;
    case 'hand': seated(); ar = -Math.PI + 0.2 + Math.sin(t * 10) * 0.1; al = -0.9; break;
    case 'work': al = -1.1 + Math.sin(t * 7) * 0.25; ar = -1.1 + Math.cos(t * 7) * 0.25; hx = 0.25 + Math.sin(t * 3) * 0.05; break;
    case 'write': ar = -2.3 + Math.sin(t * 6) * 0.25; arz = Math.sin(t * 3) * 0.3; al = -0.2; hx = -0.1; break;
    case 'wave': ar = -Math.PI + 0.3; arz = 0.4 + Math.sin(t * 8) * 0.4; al = -0.1; by = Math.abs(Math.sin(t * 4)) * 0.06; break;
    case 'cheer': { const j = Math.abs(Math.sin(t * 5)); by = j * 0.45; al = ar = -Math.PI + 0.4; alz = -0.3; arz = 0.3; lx = rx = -j * 0.4; break; }
    case 'desk': seated(); al = ar = -0.6; hx = 0.25; hy = Math.sin(t * 0.5) * 0.4; break;
    case 'sip': { seated(); al = -0.6; const up = (Math.sin(t * 0.9) + 1) / 2 > 0.75; ar = up ? -2.3 : -0.9; arz = up ? 0.35 : 0; hx = up ? -0.2 : 0.1; break; }
    case 'eat': { seated(); al = -0.6; const up = Math.sin(t * 1.3) > 0.3; ar = up ? -2.4 : -1; arz = up ? 0.3 : 0; hx = up ? 0.05 : 0.15; break; }
    case 'water': al = -0.2; ar = -1.3 + Math.sin(t * 2) * 0.1; hx = 0.3; break;
    case 'look': al = ar = 0.3; alz = -0.2; arz = 0.2; hy = Math.sin(t * 0.4) * 0.5; break;
    case 'swing': seated(); al = ar = -2.9; lx = rx = -Math.PI / 2 + Math.sin(t * 2.4) * 0.5; break;
    case 'ride': seated(); al = ar = -1.4; hy = 0.4; break;
    case 'hold': al = ar = -1.5; alz = -0.25; arz = 0.25; by = Math.abs(Math.sin(t * 3)) * 0.03; hy = Math.sin(t * 0.9) * 0.4; break;
    case 'dig': lx = rx = -Math.PI / 2; by = -0.55; al = -1 + Math.sin(t * 6) * 0.5; ar = -1 + Math.cos(t * 6) * 0.5; hx = 0.4; break;
    case 'kick': { const s = Math.sin(t * 5); rx = s > 0.6 ? -1.2 : 0.2; lx = 0.15; al = 0.4; ar = -0.4; by = 0.02; break; }
    case 'climb': { const s = Math.sin(t * 8); lx = -0.9 + s * 0.4; rx = -0.9 - s * 0.4; al = -2.6 + s * 0.3; ar = -2.6 - s * 0.3; break; }
    case 'slide': seated(); al = ar = -2.8; alz = -0.3; arz = 0.3; break;
    default: al = Math.sin(t * 1.5) * 0.05; ar = -al; hy = Math.sin(t * 0.7) * 0.25;
  }
  const e = 0.25;
  legL.rotation.x = lerp(legL.rotation.x, lx, e); legR.rotation.x = lerp(legR.rotation.x, rx, e);
  armL.rotation.x = lerp(armL.rotation.x, al, e); armR.rotation.x = lerp(armR.rotation.x, ar, e);
  armL.rotation.z = lerp(armL.rotation.z, alz, e); armR.rotation.z = lerp(armR.rotation.z, arz, e);
  body.position.y = lerp(body.position.y, by, 0.4);
  body.rotation.x = lerp(body.rotation.x, brx, 0.15);
  head.rotation.y = lerp(head.rotation.y, hy, 0.1); head.rotation.x = lerp(head.rotation.x, hx, 0.1);
}
const HIP = (0.8 - 0.06) * 0.66; // a seated kid's hips above its feet

// ---------- the furniture ----------
function desk() {
  const g = new THREE.Group();
  g.add(at(box(1.15, 0.06, 0.7, 0xd9a066), 0, 0.72, 0));
  for (const [x, z] of [[-0.5, -0.28], [0.5, -0.28], [-0.5, 0.28], [0.5, 0.28]]) g.add(at(box(0.05, 0.72, 0.05, 0x6b7280), x, 0.36, z));
  const chair = new THREE.Group();
  chair.add(at(box(0.48, 0.05, 0.44, 0x3e8ef7), 0, 0.45, 0));
  chair.add(at(box(0.48, 0.45, 0.05, 0x3e8ef7), 0, 0.7, 0.22));
  for (const [x, z] of [[-0.2, -0.18], [0.2, -0.18], [-0.2, 0.18], [0.2, 0.18]]) chair.add(at(box(0.04, 0.45, 0.04, 0x6b7280), x, 0.22, z));
  g.add(at(chair, 0, 0, 0.62));
  g.add(at(box(0.32, 0.015, 0.24, 0xffffff), -0.15, 0.76, 0.05));
  g.add(at(box(0.18, 0.02, 0.02, 0xf5a524), 0.2, 0.765, 0.05));
  return g;
}
function bookshelf() {
  const g = new THREE.Group();
  g.add(at(box(0.5, 2.1, 2.2, 0x8a5a33), 0, 1.05, 0));
  const cols = [0xe5484d, 0x3e8ef7, 0x30a46c, 0xf5a524, 0x8e4ec6, 0xffffff, 0x12a594];
  for (let shelf = 0; shelf < 4; shelf++) {
    g.add(at(box(0.48, 0.04, 2.1, 0x6b4a2b), 0.02, 0.25 + shelf * 0.5, 0));
    for (let i = 0; i < 12; i++) {
      const h = 0.3 + (hash('b' + shelf + i) % 10) / 100;
      g.add(at(box(0.32, h, 0.12, cols[hash('c' + shelf + i) % cols.length]), 0.1, 0.27 + shelf * 0.5 + h / 2, -0.95 + i * 0.165));
    }
  }
  return g;
}
function computers() {
  const g = new THREE.Group(), screens = [];
  g.add(at(box(0.9, 0.06, 2.4, 0xe9e5dc), 0, 0.75, 0));
  for (const [x, z] of [[-0.35, -1.1], [0.35, -1.1], [-0.35, 1.1], [0.35, 1.1]]) g.add(at(box(0.05, 0.75, 0.05, 0x6b7280), x, 0.37, z));
  for (const z of [-0.6, 0.6]) {
    g.add(at(box(0.1, 0.06, 0.3, 0x222222), -0.1, 0.8, z));
    g.add(at(box(0.06, 0.25, 0.06, 0x222222), -0.15, 0.93, z));
    g.add(at(box(0.06, 0.5, 0.8, 0x1f2430), -0.15, 1.25, z));
    const scr = new THREE.Mesh(new THREE.PlaneGeometry(0.72, 0.42), new THREE.MeshStandardMaterial({ color: 0x0b1020, emissive: 0x2fd27a, emissiveIntensity: 0.25 }));
    scr.rotation.y = Math.PI / 2;
    g.add(at(scr, -0.115, 1.25, z));
    g.add(at(box(0.25, 0.02, 0.5, 0x2a2a2a), 0.2, 0.79, z));
    screens.push(scr);
  }
  g.userData.screens = screens;
  return g;
}
function lab() {
  const g = new THREE.Group(), liquids = [];
  g.add(at(box(0.9, 0.9, 2.4, 0xf0f0f0), 0, 0.45, 0));
  g.add(at(box(0.95, 0.06, 2.45, 0x2f3e46), 0, 0.92, 0));
  const glass = new THREE.MeshStandardMaterial({ color: 0xffffff, transparent: true, opacity: 0.35, roughness: 0.05 });
  [[0x30a46c, -0.8], [0xe5484d, -0.3], [0x3e8ef7, 0.25], [0xf5a524, 0.8]].forEach(([c, z], i) => {
    const flask = new THREE.Group();
    flask.add(new THREE.Mesh(new THREE.SphereGeometry(0.16, 16, 12), glass));
    flask.add(at(new THREE.Mesh(new THREE.CylinderGeometry(0.04, 0.05, 0.22, 10), glass), 0, 0.2, 0));
    const liq = new THREE.Mesh(new THREE.SphereGeometry(0.14, 16, 12, 0, Math.PI * 2, Math.PI * 0.45, Math.PI * 0.55), new THREE.MeshStandardMaterial({ color: c, emissive: c, emissiveIntensity: 0.4 }));
    flask.add(liq);
    liquids.push(liq);
    g.add(at(flask, i % 2 ? 0.15 : -0.15, 1.12, z));
  });
  g.add(at(box(0.25, 0.05, 0.3, 0x333333), -0.2, 0.97, 1.1));
  g.add(at(box(0.06, 0.4, 0.06, 0x333333), -0.28, 1.2, 1.1));
  g.add(at(new THREE.Mesh(new THREE.CylinderGeometry(0.05, 0.05, 0.3, 12), mat(0xdddddd)), -0.2, 1.35, 1.1).rotateZ(0.5));
  g.userData.liquids = liquids;
  return g;
}
const globeTex = (() => {
  const c = document.createElement('canvas'); c.width = 256; c.height = 128;
  const x = c.getContext('2d');
  x.fillStyle = '#2b7bd6'; x.fillRect(0, 0, 256, 128);
  x.fillStyle = '#5fb35a';
  for (let i = 0; i < 40; i++) { x.beginPath(); x.ellipse(hash('gx' + i) % 256, 20 + (hash('gy' + i) % 88), 6 + hash('gr' + i) % 20, 4 + hash('gs' + i) % 12, 0, 0, 7); x.fill(); }
  const t = new THREE.CanvasTexture(c); t.colorSpace = THREE.SRGBColorSpace; return t;
})();
function globe() {
  const g = new THREE.Group();
  g.add(at(box(0.6, 0.8, 0.6, 0x8a5a33), 0, 0.4, 0));
  g.add(at(cyl(0.03, 0.03, 0.4, 0xc0a060, 8), 0, 0.95, 0));
  const ball = shade(new THREE.Mesh(new THREE.SphereGeometry(0.38, 32, 20), new THREE.MeshStandardMaterial({ map: globeTex, roughness: 0.5 })));
  ball.rotation.z = 0.4;
  g.add(at(ball, 0, 1.3, 0));
  g.userData.ball = ball;
  return g;
}
function board() {
  const c = document.createElement('canvas'); c.width = 1024; c.height = 340;
  const tex = new THREE.CanvasTexture(c); tex.colorSpace = THREE.SRGBColorSpace; tex.anisotropy = 8;
  const g = new THREE.Group();
  g.add(at(box(6.3, 2.3, 0.08, 0x8a5a33), 0, 0, -0.03));
  g.add(at(new THREE.Mesh(new THREE.PlaneGeometry(6, 2), new THREE.MeshStandardMaterial({ map: tex, roughness: 0.95 })), 0, 0, 0.02));
  g.add(at(box(6.1, 0.06, 0.18, 0x8a5a33), 0, -1.08, 0.09));
  g.userData = { c, tex, text: '', shown: -1 };
  return g;
}
function wrapText(x, text, w) {
  const out = []; let line = '';
  for (const word of text.split(/\s+/)) {
    const t = line ? line + ' ' + word : word;
    if (x.measureText(t).width > w && line) { out.push(line); line = word; } else line = t;
  }
  if (line) out.push(line);
  return out;
}
function drawBoard(b, title, lesson, n) {
  const { c } = b.userData, x = c.getContext('2d');
  x.fillStyle = '#24493a'; x.fillRect(0, 0, c.width, c.height);
  for (let i = 0; i < 120; i++) { x.fillStyle = `rgba(255,255,255,${0.02 + (hash('d' + i) % 4) / 100})`; x.fillRect(hash('x' + i) % 1024, hash('y' + i) % 340, 40, 3); }
  x.fillStyle = '#f4f1e6'; x.font = 'bold 46px "Comic Sans MS", "Chalkboard SE", "Comic Neue", cursive';
  x.fillText(wrapText(x, title, 960)[0] || '', 32, 66);
  x.fillRect(32, 82, Math.min(960, x.measureText(title).width), 3);
  x.font = '34px "Comic Sans MS", "Chalkboard SE", "Comic Neue", cursive';
  x.fillStyle = '#e8e4d4';
  wrapText(x, lesson.slice(0, n), 960).slice(-5).forEach((l, i) => x.fillText(l, 32, 135 + i * 42));
  b.userData.tex.needsUpdate = true;
}
function signTex(text) {
  const c = document.createElement('canvas'); c.width = 1024; c.height = 160;
  const x = c.getContext('2d');
  x.fillStyle = '#2f3e66'; x.fillRect(0, 0, 1024, 160);
  x.fillStyle = '#fff6dd'; x.font = 'bold 84px ui-rounded, "Nunito", sans-serif'; x.textAlign = 'center';
  x.fillText(text, 512, 112);
  const t = new THREE.CanvasTexture(c); t.colorSpace = THREE.SRGBColorSpace; return t;
}

// ---------- the school: a room per agent ----------
const classes = new Map();    // id → class
const actors = new Map();     // kid id → kid
let order = [];               // class ids, first seen first
let first = true;             // the first snapshot places everyone without walking
const school = new THREE.Group(); // its roof, facade and sign, sized by layout
scene.add(school);

function buildRoom(cls) {
  const g = new THREE.Group();
  const hue = new THREE.Color().setHSL((hash(cls.id) % 360) / 360, 0.55, 0.55).getHex();
  g.add(at(box(W, 0.1, D, 0xd2a679), 0, 0.05, 0));
  for (let i = 0; i < 7; i++) g.add(at(box(W, 0.004, 0.02, 0xb98d5f), 0, 0.102, -D / 2 + (i + 1) * D / 8));
  g.add(at(box(W + 0.3, WALL, 0.25, 0xf3ead8), 0, WALL / 2, -D / 2 - 0.12));
  g.add(at(box(W + 0.3, 1, 0.26, hue), 0, 0.5, -D / 2 - 0.12));
  for (const s of [-1, 1]) {
    g.add(at(box(0.25, WALL, D, 0xf3ead8), s * (W / 2 + 0.02), WALL / 2, 0));
    g.add(at(box(0.26, 1, D, hue), s * (W / 2 + 0.02), 0.5, 0));
  }
  for (const z of [-3, 1]) {
    g.add(at(box(0.28, 1.2, 2.2, 0xffffff), -W / 2, 1.75, z));
    const pane = new THREE.Mesh(new THREE.PlaneGeometry(2, 1), new THREE.MeshStandardMaterial({ color: 0xbfe6ff, emissive: 0x6fb6ff, emissiveIntensity: 0.35 }));
    pane.rotation.y = Math.PI / 2;
    g.add(at(pane, -W / 2 + 0.16, 1.75, z));
  }
  const fw = DOOR_X - 0.9 + W / 2;
  g.add(at(box(fw, 0.7, 0.2, 0xf3ead8), -W / 2 + fw / 2, 0.35, D / 2));
  g.add(at(box(W / 2 - DOOR_X - 0.9, 0.7, 0.2, 0xf3ead8), (DOOR_X + 0.9 + W / 2) / 2, 0.35, D / 2));
  for (const s of [-1, 1]) g.add(at(box(0.2, 2.3, 0.25, 0x8a5a33), DOOR_X + s * 0.9, 1.15, D / 2));
  g.add(at(box(2, 0.2, 0.25, 0x8a5a33), DOOR_X, 2.3, D / 2));
  const b = at(board(), 0, 1.6, -D / 2 + 0.02);
  g.add(b);
  // her desk, with an apple and a mug she picks up
  const td = new THREE.Group();
  td.add(at(box(2.4, 0.08, 1, 0x8a5a33), 0, 0.78, 0));
  td.add(at(box(2.4, 0.7, 0.06, 0x6b4a2b), 0, 0.4, 0.45));
  for (const s of [-1, 1]) td.add(at(box(0.06, 0.74, 0.9, 0x6b4a2b), s * 1.15, 0.37, 0));
  const apple = at(sphere(0.09, 0xd7262d), 0.7, 0.9, 0.1); td.add(apple);
  const mug = at(cyl(0.07, 0.06, 0.15, 0xffffff), -0.6, 0.9, 0.05); td.add(mug);
  td.add(at(box(0.5, 0.08, 0.35, 0x3e8ef7), 0, 0.86, -0.1));
  const tchair = new THREE.Group();
  tchair.add(at(box(0.55, 0.06, 0.5, 0x2f3e66), 0, 0.5, 0));
  tchair.add(at(box(0.55, 0.6, 0.06, 0x2f3e66), 0, 0.82, -0.25));
  td.add(at(tchair, 0, 0, -0.75));
  g.add(at(td, 0, 0, -D / 2 + 2.6));
  const clock = cyl(0.3, 0.3, 0.05, 0xffffff, 24); clock.rotation.x = Math.PI / 2;
  g.add(at(clock, 4.6, 2.3, -D / 2 + 0.05));
  const hand = at(box(0.03, 0.22, 0.02, 0x222222), 4.6, 2.3, -D / 2 + 0.1); g.add(hand);
  const pot = new THREE.Group();
  pot.add(at(cyl(0.25, 0.18, 0.4, 0xc2410c), 0, 0.2, 0));
  pot.add(at(shade(new THREE.Mesh(new THREE.IcosahedronGeometry(0.4, 1), mat(0x3f8f3a, { flatShading: true }))), 0, 0.7, 0));
  g.add(at(pot, -W / 2 + 0.6, 0, -D / 2 + 0.6));
  const shelf = at(bookshelf(), -W / 2 + 0.4, 0, -1.6); g.add(shelf);
  const pcs = at(computers(), -W / 2 + 0.6, 0, 3); g.add(pcs);
  const bench = at(lab(), W / 2 - 0.6, 0, -1.6); g.add(bench);
  const glb = at(globe(), W / 2 - 0.6, 0, 2.6); g.add(glb);
  const spots = {
    books: new THREE.Vector3(-W / 2 + 1.5, 0, -1.6), computers: new THREE.Vector3(-W / 2 + 1.6, 0, 3),
    lab: new THREE.Vector3(W / 2 - 1.7, 0, -1.6), web: new THREE.Vector3(W / 2 - 1.6, 0, 2.6),
    teacher: new THREE.Vector3(0, 0, -D / 2 + 3.6),
  };
  const face = { books: -Math.PI / 2, computers: -Math.PI / 2, lab: Math.PI / 2, web: Math.PI / 2, teacher: Math.PI };
  const lamp = new THREE.PointLight(0xffe2b0, 4, 18, 1.6);
  g.add(at(lamp, 0, WALL + 0.6, 0));
  // the teacher, her things in hand, her label and bubbles
  const teacher = person({ seed: cls.id + 't', shirt: 0xd6409f, teacher: true });
  teacher.userData.kind = 'teacher';
  teacher.userData.id = cls.id;
  const inHand = { mug: at(cyl(0.07, 0.06, 0.15, 0xffffff), 0, -0.05, 0.08), apple: at(sphere(0.09, 0xd7262d), 0, -0.05, 0.1) };
  for (const o of Object.values(inHand)) { o.visible = false; teacher.userData.parts.armR.userData.hand.add(o); }
  const tl = label('', 'label teacher');
  teacher.add(at(tl, 0, 2.2, 0));
  const bubble = label('', 'bubble');
  teacher.add(at(bubble, 0, 2.7, 0));
  const doing = label('', 'emoji');
  teacher.add(at(doing, 0.55, 2.05, 0));
  g.add(teacher);
  at(teacher, 0, 0.1, -D / 2 + 1.9);
  scene.add(g);
  const c = {
    id: cls.id, g, b, spots, face, lamp, hand, pcs, bench, glb, teacher, tl, bubble, doing, apple, mug, inHand, hue,
    desks: [], seats: new Map(), data: cls, x: 0, chalk: 0, pace: 0,
    routine: { kind: '', until: 0, path: [], i: hash(cls.id) % 5 },
  };
  for (let i = 0; i < SEATS; i++) {
    const d = desk();
    at(d, (i % COLS - (COLS - 1) / 2) * 2.7 + 0.2, 0.1, -1.5 + Math.floor(i / COLS) * 2.05);
    d.visible = false;
    g.add(d);
    c.desks.push(d);
  }
  return c;
}
// ---------- getting around a room ----------
// One plan for every room, they are all alike, in the room's own coordinates:
// the desks and their chairs, her desk and chair, the furniture by the walls.
const V3 = (x = 0, y = 0, z = 0) => new THREE.Vector3(x, y, z);
const classNav = new Nav(-W / 2 + 0.1, -D / 2 + 0.1, W / 2 - 0.1, D / 2 - 0.12, 0.15, 0.25);
for (let i = 0; i < SEATS; i++) {
  const x = (i % COLS - (COLS - 1) / 2) * 2.7 + 0.2, z = -1.5 + Math.floor(i / COLS) * 2.05;
  classNav.box('desk', x - 0.58, z - 0.36, x + 0.58, z + 0.36);
  classNav.box('seat' + i, x - 0.25, z + 0.4, x + 0.25, z + 0.85);
}
classNav.box('tdesk', -1.22, -D / 2 + 2.08, 1.22, -D / 2 + 3.12);
classNav.box('tchair', -0.3, -D / 2 + 1.6, 0.3, -D / 2 + 2.1);
classNav.box('furn', -W / 2, -2.75, -W / 2 + 0.68, -0.45);   // the bookshelf
classNav.box('furn', -W / 2, 1.78, -W / 2 + 1.07, 4.22);     // the computers
classNav.box('furn', W / 2 - 1.07, -2.83, W / 2, -0.37);     // the lab
classNav.box('furn', W / 2 - 0.92, 2.28, W / 2, 2.92);       // the globe
classNav.circle('furn', -W / 2 + 0.6, -D / 2 + 0.6, 0.42);  // the plant
const DOOR_IN = V3(DOOR_X, 0.1, D / 2 - 0.55), DOOR_OUT = V3(DOOR_X, 0.06, HALL_Z);

function seatPos(c, i) { const d = c.desks[Math.min(i, SEATS - 1)].position; return V3(d.x, 0.1, d.z + 0.62); }
function freeSeat(c) {
  const used = new Set(c.seats.values());
  for (let i = 0; i < SEATS; i++) if (!used.has(i)) return i;
  return -1;
}
function seatSkip(kid) { return kid.seat >= 0 ? classNav.bit('seat' + kid.seat) : 0; }
function station(tool) {
  const t = (tool || '').split(/\s/)[0];
  if (/^(Read|Grep|Glob|LS|NotebookRead)$/.test(t)) return 'books';
  if (/^(Bash|BashOutput|KillShell|Monitor)$/.test(t)) return 'computers';
  if (/^(Edit|Write|MultiEdit|NotebookEdit)$/.test(t)) return 'lab';
  if (/^(WebFetch|WebSearch)$/.test(t) || t.startsWith('mcp__')) return 'web';
  if (/^(Task|Agent)$/.test(t)) return 'teacher';
  return '';
}
// go sends a kid round whatever is in the way to `to`, then on through `then`
function go(kid, nav, to, skip, ...then) { kid.path = [...nav.path(kid.p.position, to, skip), ...then]; kid.skip = skip; }
function face(p, ry, k = 0.2) { p.rotation.y = lerpAngle(p.rotation.y, ry, k); }

// ---------- the park: who plays what ----------
const PLAYS = [['swing', SWING.n], ['slide', 3], ['seesaw', 4], ['merry', 4], ['sand', 4], ['football', 6], ['tag', 10]];
function freeSlot(kind, cap) {
  const used = new Set([...actors.values()].filter(a => a.play === kind).map(a => a.slot));
  for (let i = 0; i < cap; i++) if (!used.has(i)) return i;
  return -1;
}
// assign picks a game with room for one more; with every game full, it
// watches the football
function assign(kid) {
  if (kid.play) return;
  const start = hash(kid.id) % PLAYS.length;
  for (let i = 0; i < PLAYS.length; i++) {
    const [kind, cap] = PLAYS[(start + i) % PLAYS.length];
    const s = freeSlot(kind, cap);
    if (s >= 0) { kid.play = kind; kid.slot = s; return; }
  }
  kid.play = 'watch'; kid.slot = Math.max(0, freeSlot('watch', 18));
}
// parkSkip is the game a kid may walk into: its swing, its seesaw, the pitch…
function parkSkip(kid) {
  const tag = { swing: 'swing' + kid.slot, slide: 'slide', seesaw: 'seesaw' + (kid.slot >> 1), merry: 'merry', sand: 'sand', football: 'pitch' }[kid.play];
  return tag ? parkNav.bit(tag) : 0;
}
const footSpots = [[-4, -1.5], [-2, 1.5], [3.5, -1.8], [1.5, 1.6], [-4.5, 1.8], [4.5, 0.5]];
const sandAngle = s => s * Math.PI / 2 + 0.6;
const merryAngle = s => s * Math.PI / 2 + Math.PI / 4;
// the slide's queue fans out from the mark, so whoever's turn it is walks to
// it past nobody
function slideWait(s) { const a = [Math.PI, Math.PI * 1.2, Math.PI * 0.83][s % 3]; return V3(SL.start.x + Math.cos(a) * 1.45, 0.06, SL.start.z + Math.sin(a) * 1.45); }
function merryQueue(s) { const a = merryAngle(s); return V3(MERRY.x + Math.cos(a) * 2.8, 0.06, MERRY.z + Math.sin(a) * 2.8); }
function tagPoint() { return V3(lerp(TAG.x0 + 1, TAG.x1 - 1, Math.random()), 0.06, lerp(TAG.z0 + 1, TAG.z1 - 1, Math.random())); }
function watchSpot(s) { return V3(WATCH.x + (s % 6) * 1.6, 0.06, WATCH.z + Math.floor(s / 6) * 0.8); }
// approach is where a kid walks to start its game: the last point may be
// inside the game (into the sandbox, onto the pitch)
function approach(kid) {
  const s = Math.max(0, kid.slot);
  switch (kid.play) {
    case 'swing': return [V3(swings[s].position.x, 0.06, SWING.z + 0.35)];
    case 'slide': return [slideWait(s)];
    case 'seesaw': { const cfg = SEESAW[s >> 1], side = s % 2 ? 1 : -1; return [V3(cfg.x + side * 1.6, 0.06, cfg.z + 0.45)]; }
    case 'merry': return [merryQueue(s)];
    case 'sand': {
      const a = sandAngle(s), c = Math.cos(a), n = Math.sin(a), r = 3.15 / Math.max(Math.abs(c), Math.abs(n));
      return [V3(SAND.x + c * r, 0.06, SAND.z + n * r), V3(SAND.x + c * 1.6, 0.25, SAND.z + n * 1.6)];
    }
    case 'football': { const [dx, dz] = footSpots[s % footSpots.length]; return [V3(PITCH.x + dx, 0.06, PITCH.z + dz)]; }
    case 'tag': return [tagPoint()];
    default: return [watchSpot(s)];
  }
}
// enterPark walks a kid in through the gate and round everything to its game
function enterPark(kid, via) {
  const a = approach(kid), skip = parkSkip(kid);
  kid.path = [...via, GATE_IN.clone(), ...parkNav.path(GATE_IN, a[0], skip), ...a.slice(1)];
  kid.skip = skip; kid.on = false; kid.gs = '';
}
// leavePark gets a kid off its game and back to its class
function leavePark(kid) {
  const p = kid.p, c = kid.cls, skip = kid.skip;
  if (p.parent !== park) park.attach(p);
  p.rotation.set(0, p.rotation.y, 0);
  if (game.holder === kid) game.holder = null;
  if (chase.it === kid) chase.it = null;
  kid.play = ''; kid.slot = -1; kid.on = false; kid.gs = '';
  kid.stage = 'toClass';
  kid.path = [...parkNav.path(p.position, GATE_IN, skip), GATE.clone(), V3(0, 0.06, HALL_Z), V3(c.x + DOOR_X, 0.06, HALL_Z)];
}
// mount puts a kid on a moving thing, the swing's seat, the seesaw's plank,
// the merry-go-round: it moves with it from then on
function mount(kid, thing, pos, ry, t) { thing.attach(kid.p); kid.on = true; kid.mount = { pos, ry, t }; }
function settle(kid, dt) {
  const p = kid.p, m = kid.mount;
  p.position.lerp(m.pos, Math.min(1, dt * 5));
  p.rotation.x = lerp(p.rotation.x, 0, 0.2); p.rotation.z = lerp(p.rotation.z, 0, 0.2);
  face(p, m.ry);
}
const seated = (kid, t) => kid.on && t - kid.mount.t > 0.8;
// stride walks a kid toward a point that moves; standing, it looks to ry
function stride(kid, to, max, dt, t, ry, still) {
  const p = kid.p, d = to.clone().sub(p.position); d.y = 0;
  const len = d.length(), step = Math.min(len, max * dt), speed = step / dt;
  if (len > 1e-3) p.position.addScaledVector(d.normalize(), step);
  p.position.y = lerp(p.position.y, to.y, 0.2);
  if (speed > 0.35) { face(p, Math.atan2(d.x, d.z)); pose(p, speed > 1.7 ? 'run' : 'walk', t + hash(kid.id) % 9, Math.max(0.5, speed / 1.9)); }
  else { face(p, ry, 0.15); pose(p, still, t + hash(kid.id) % 9); }
}

// the slide's round: to the ladder, up, sit, down the ramp, and run back
// round it; three kids go round a third of a round apart
const SL = (() => {
  const L = SLIDE, ramp = z => 2.15 - (z - 0.63) * Math.tan(0.45) + 0.05 - HIP;
  const pts = [
    [L.x, 0.06, L.z - 2.6, 'walk', 1.4], [L.x, 0.06, L.z - 1.7, 'climb', 1.2], [L.x, 2.27, L.z - 0.55, 'walk', 1.4],
    [L.x, 2.27, L.z + 0.45, 'slide', 0.5], [L.x, ramp(0.8), L.z + 0.8, 'slide', 3.2], [L.x, ramp(4.65), L.z + 4.65, 'run', 2.6],
    [L.x, 0.06, L.z + 5.6, 'run', 2.6], [L.x + 1.3, 0.06, L.z + 5.6, 'run', 2.6], [L.x + 1.3, 0.06, L.z - 2.6, 'run', 2.6], [L.x, 0.06, L.z - 2.6]];
  const segs = [];
  let T = 0;
  for (let i = 0; i < pts.length - 1; i++) {
    const a = V3(...pts[i].slice(0, 3)), b = V3(...pts[i + 1].slice(0, 3)), dur = a.distanceTo(b) / pts[i][4];
    segs.push({ a, b, pose: pts[i][3], t0: T, dur, ry: Math.hypot(b.x - a.x, b.z - a.z) > 0.01 ? Math.atan2(b.x - a.x, b.z - a.z) : 0 });
    T += dur;
  }
  return { segs, T, start: segs[0].a };
})();
function slideAt(f) {
  const t = f * SL.T, s = SL.segs.find(s => t < s.t0 + s.dur) || SL.segs[SL.segs.length - 1];
  return { p: s.a.clone().lerp(s.b, Math.min(1, (t - s.t0) / s.dur)), ry: s.ry, pose: s.pose };
}
function slideStep(kid, dt, t) {
  const p = kid.p, f = ((t / SL.T) + kid.slot / 3) % 1;
  if (kid.gs === 'loop') { const g = slideAt(f); p.position.copy(g.p); face(p, g.ry, 0.3); pose(p, g.pose, t); kid.on = true; return; }
  face(p, 0); pose(p, 'idle', t + hash(kid.id) % 5);
  if (kid.gs === 'ready') { if (f < kid.lastF) kid.gs = 'loop'; kid.lastF = f; return; } // on the mark: off when its turn comes
  // in the queue, until it is time to walk to the mark
  const need = Math.hypot(p.position.x - SL.start.x, p.position.z - SL.start.z) / 2.6 + 0.4, left = (1 - f) * SL.T;
  if (left < need && left > need - 0.3) { kid.path = [SL.start.clone()]; kid.gs = 'ready'; kid.lastF = f; }
}

// the swings, the seesaws and the merry-go-round move with who is on them
const swingState = swings.map(() => ({ amp: 0, ph: 0 }));
const sawState = planks.map(() => ({ ph: 0 }));
const merryState = { speed: 0 };
function props(dt, t) {
  const on = new Map();
  let riders = 0, waiting = 0;
  for (const a of actors.values()) {
    if (a.stage !== 'park') continue;
    if (a.play === 'merry') { if (a.on) riders++; else if (a.gs === 'board' || !a.path.length) waiting++; }
    if (a.on && (a.play === 'swing' || a.play === 'seesaw')) on.set(a.play + a.slot, a);
  }
  swings.forEach((pv, i) => { // a swing only swings with a kid on it, pushing higher and higher
    const st = swingState[i], k = on.get('swing' + i);
    if (k && seated(k, t) && !k.leaving) st.amp = Math.min(0.75, st.amp + dt * 0.2); else st.amp *= Math.exp(-dt * (k ? 1.6 : 0.9));
    st.ph = st.amp < 1e-3 ? i * 1.7 : st.ph + dt * 2.2; // each its own rhythm
    pv.rotation.x = st.amp * Math.sin(st.ph);
  });
  planks.forEach((pl, j) => { // two on it rock it; one alone sits down on its end
    const l = on.get('seesaw' + 2 * j), r = on.get('seesaw' + (2 * j + 1)), st = sawState[j];
    let b = 0;
    if (l && r) { st.ph += dt * 1.7; b = Math.sin(st.ph) * 0.28; } else if (l) b = 0.25; else if (r) b = -0.25;
    pl.rotation.z = lerp(pl.rotation.z, b, Math.min(1, dt * 3));
  });
  // it spins with riders, and stops for whoever wants to get on
  const want = waiting ? 0 : riders ? 0.9 : 0;
  merryState.speed = lerp(merryState.speed, want, Math.min(1, dt * (want ? 0.6 : 1.2)));
  if (!want && merryState.speed < 0.005) merryState.speed = 0;
  merry.rotation.y -= merryState.speed * dt;
}

// parkStep is a kid at its game
function parkStep(kid, dt, t) {
  const p = kid.p, s = Math.max(0, kid.slot), ph = hash(kid.id) % 7;
  text(kid.fx, '');
  switch (kid.play) {
    case 'swing': {
      const sw = swingState[s];
      if (kid.on && kid.leaving && sw.amp < 0.05) { kid.leaving = false; leavePark(kid); return; }
      if (kid.on) { settle(kid, dt); pose(p, 'swing', (sw.ph + Math.PI / 2) / 2.4); return; }
      face(p, 0); pose(p, 'idle', t + ph); // its back to the seat, until the swing is still
      if (sw.amp < 0.04) mount(kid, swings[s], V3(0, -2 + 0.03 - HIP, -0.05), 0, t);
      return;
    }
    case 'seesaw': {
      const side = s % 2 ? 1 : -1;
      if (!kid.on) mount(kid, planks[s >> 1], V3(side * 1.6, 0.1 - HIP, 0), side > 0 ? -Math.PI / 2 : Math.PI / 2, t);
      settle(kid, dt); pose(p, 'ride', t + ph);
      return;
    }
    case 'merry': {
      const a = merryAngle(s);
      if (kid.on) { settle(kid, dt); pose(p, 'hold', t + ph); return; }
      if (kid.gs === 'board') { // at its place on the still merry-go-round
        mount(kid, merry, V3(Math.cos(a) * MERRY.r, 0.38, Math.sin(a) * MERRY.r), Math.atan2(-Math.cos(a), -Math.sin(a)), t);
        kid.gs = '';
        return;
      }
      face(p, Math.atan2(MERRY.x - p.position.x, MERRY.z - p.position.z)); pose(p, 'idle', t + ph);
      if (merryState.speed === 0 && ![...actors.values()].some(o => o.play === 'merry' && o.gs === 'board')) {
        const w = a - merry.rotation.y; // its place, where the merry-go-round stopped
        kid.path = [...parkNav.path(p.position, V3(MERRY.x + Math.cos(w) * 2.45, 0.06, MERRY.z + Math.sin(w) * 2.45), 0),
          V3(MERRY.x + Math.cos(w) * MERRY.r, 0.38, MERRY.z + Math.sin(w) * MERRY.r)];
        kid.gs = 'board';
      }
      return;
    }
    case 'slide': slideStep(kid, dt, t); return;
    case 'sand': face(p, Math.atan2(SAND.x - p.position.x, SAND.z - p.position.z)); pose(p, 'dig', t + ph); kid.on = true; return;
    case 'football': {
      const [dx, dz] = footSpots[s % footSpots.length];
      const to = V3(PITCH.x + dx + Math.sin(t * 0.7 + ph) * 0.8, 0.06, PITCH.z + dz + Math.cos(t * 0.5 + ph) * 0.5);
      stride(kid, to, 2.4, dt, t, Math.atan2(ball.position.x - p.position.x, ball.position.z - p.position.z), game.kicker === kid && game.t < 0.3 ? 'kick' : 'idle');
      kid.on = true;
      return;
    }
    case 'tag': kid.on = true; return; // tagGame runs them all
    default: stride(kid, watchSpot(s), 2, dt, t, Math.PI, ball.position.y > 0.9 ? 'cheer' : 'look'); kid.on = true;
  }
}

// tag: one chases, the rest run away; whoever is caught chases next
const chase = { it: null, until: 0 };
function tagGame(dt, t) {
  const all = [...actors.values()].filter(a => a.play === 'tag' && a.stage === 'park'), ps = all.filter(a => !a.path.length);
  if (!ps.length) return;
  if (!ps.includes(chase.it)) { chase.it = ps[0]; chase.until = t + 1.5; }
  const it = chase.it, push = V3();
  for (const k of ps) {
    const p = k.p.position, want = V3();
    k.v = k.v || V3();
    if (k === it && ps.length > 1) {
      if (t > chase.until) { // counted to three: after the nearest
        let near = null, nd = Infinity;
        for (const o of ps) if (o !== k) { const d = o.p.position.distanceTo(p); if (d < nd) { nd = d; near = o; } }
        want.subVectors(near.p.position, p).setY(0).setLength(3.1);
        if (nd < 0.65) { chase.it = near; chase.until = t + 1.5; }
      }
    } else if (it !== k && p.distanceTo(it.p.position) < 4.5) want.subVectors(p, it.p.position).setY(0).setLength(2.7);
    else { // nobody after it: it wanders about
      if (!k.wander || k.wander.distanceTo(p) < 0.6) k.wander = tagPoint();
      want.subVectors(k.wander, p).setY(0).setLength(1.1);
    }
    const m = 1.2; // away from the edges of the lawn, and from each other
    if (p.x < TAG.x0 + m) want.x += (TAG.x0 + m - p.x) * 3;
    if (p.x > TAG.x1 - m) want.x -= (p.x - TAG.x1 + m) * 3;
    if (p.z < TAG.z0 + m) want.z += (TAG.z0 + m - p.z) * 3;
    if (p.z > TAG.z1 - m) want.z -= (p.z - TAG.z1 + m) * 3;
    for (const o of all) { // the ones still coming onto the lawn too
      if (o === k) continue;
      const d = p.distanceTo(o.p.position);
      if (d < 0.8 && d > 1e-3) want.addScaledVector(push.subVectors(p, o.p.position).setY(0).normalize(), (0.8 - d) * 8);
    }
    k.v.lerp(want, Math.min(1, dt * 4));
  }
  for (const k of ps) {
    const p = k.p.position;
    p.addScaledVector(k.v, dt);
    p.x = Math.min(TAG.x1, Math.max(TAG.x0, p.x)); p.z = Math.min(TAG.z1, Math.max(TAG.z0, p.z));
    p.y = lerp(p.y, 0.06, 0.2);
    const sp = k.v.length();
    if (sp > 0.3) face(k.p, Math.atan2(k.v.x, k.v.z), 0.25);
    pose(k.p, sp > 1.6 ? 'run' : sp > 0.3 ? 'walk' : 'idle', t + hash(k.id) % 9, Math.max(0.5, sp / 1.9));
    text(k.fx, k === chase.it && ps.length > 1 ? '🫵' : '');
  }
}

// ---------- kids ----------
let arrivals = 0; // kids that came in with this snapshot: they line up instead of piling up
function newKid(c, k, arriving) {
  const p = person({ seed: k.id, shirt: shirtOf(k.type), cap: c.hue });
  p.userData.kind = 'kid';
  p.userData.id = k.id;
  const l = label(k.type, 'label hidden');
  p.add(at(l, 0, 2.75, 0));
  const fx = label('', 'emoji');
  p.add(at(fx, 0.5, 2.55, 0));
  const kid = { id: k.id, cls: c, p, l, fx, data: k, path: [], skip: 0, stage: 'class', seat: -1, done: !k.running, at: '', play: '', slot: -1, on: false, gs: '' };
  actors.set(k.id, kid);
  if (k.running) {
    c.g.add(p);
    takeSeat(kid);
    const s = seatOf(kid);
    if (arriving) { // in through the door while the teacher's note flies to the desk
      p.position.set(W / 2 + GAP / 2 + 0.9 * arrivals++, 0.06, HALL_Z);
      kid.path = [DOOR_OUT.clone(), DOOR_IN.clone(), ...classNav.path(DOOR_IN, s, seatSkip(kid))];
      kid.skip = seatSkip(kid);
      note(c, s);
    } else { p.position.copy(s); kid.skip = seatSkip(kid); }
    p.rotation.y = Math.PI;
  } else { // already out playing
    park.add(p);
    kid.stage = 'park';
    assign(kid);
    if (arriving) { // in through the gate, one behind the other
      const n = arrivals++;
      p.position.set(((n % 3) - 1) * 0.9, 0.06, PARK.z0 - 1.2 - Math.floor(n / 3) * 0.9);
      enterPark(kid, [GATE.clone()]);
    }
    else { const a = approach(kid); p.position.copy(a[a.length - 1]); kid.skip = parkSkip(kid); }
  }
  return kid;
}
function takeSeat(kid) { kid.seat = freeSeat(kid.cls); if (kid.seat >= 0) kid.cls.seats.set(kid.id, kid.seat); }
function seatOf(kid) { return kid.seat >= 0 ? seatPos(kid.cls, kid.seat) : V3(DOOR_X, 0.1, D / 2 - 0.8); }

const notes = [];
function note(c, to) {
  const n = label('📨', 'emoji');
  c.g.add(n);
  notes.push({ n, c, from: c.teacher.position.clone().setY(2), to: to.clone().setY(1.2), t: 0 });
}

// plan decides where a kid goes from its data.
function plan(kid, now) {
  const c = kid.cls, k = kid.data;
  if (!k.running) {
    if (kid.stage === 'class' && !kid.done) { // just finished: hand up, then out to the park
      kid.done = true;
      kid.handUntil = now + 1800;
    }
    return;
  }
  if (kid.done) { // running again: back to class
    kid.done = false;
    if (kid.stage === 'park' && kid.play === 'swing' && kid.on) kid.leaving = true; // it slows the swing down first
    else if (kid.stage === 'park') leavePark(kid);
    return;
  }
  if (kid.stage !== 'class' || kid.path.length) return;
  const st = station(k.tool);
  const stale = Date.now() - new Date(k.modified) > 45e3;
  const want = st && !stale ? st : '';
  if (want !== kid.at) {
    if (want) {
      const busy = [...actors.values()].filter(a => a.cls === c && a.at === want && a !== kid).length, o = ((busy % 3) - 1) * 0.75;
      go(kid, classNav, c.spots[want].clone().add(want === 'teacher' ? V3(o, 0.1, 0) : V3(0, 0.1, o)), seatSkip(kid));
    } else go(kid, classNav, seatOf(kid), seatSkip(kid));
    kid.at = want;
  }
}

function walk(kid, dt, t) {
  const p = kid.p, to = kid.path[0], d = to.clone().sub(p.position); d.y = 0;
  const dist = d.length(), v = (kid.stage === 'class' ? 1.9 : 2.6) * dt;
  // someone walking just ahead: it waits for them rather than walk into them,
  // and after a while squeezes past
  kid.waitFor = null;
  if (t > (kid.passUntil || 0)) for (const o of actors.values()) {
    if (o === kid || !o.path.length || o.p.parent !== p.parent || o.waitFor === kid) continue;
    const ox = o.p.position.x - p.position.x, oz = o.p.position.z - p.position.z, od = Math.hypot(ox, oz);
    if (od < 0.6 && ox * d.x + oz * d.z > 0.4 * od * dist) { kid.waitFor = o; break; }
  }
  if (kid.waitFor) {
    kid.waited = (kid.waited || 0) + dt;
    if (kid.waited > 1.5) { kid.passUntil = t + 0.6; kid.waited = 0; }
    pose(p, 'idle', t + hash(kid.id) % 10);
    return;
  }
  kid.waited = 0;
  if (dist > 0.01) face(p, Math.atan2(d.x, d.z), 0.3);
  p.rotation.x = lerp(p.rotation.x, 0, 0.3); p.rotation.z = lerp(p.rotation.z, 0, 0.3);
  if (dist <= v) { p.position.x = to.x; p.position.z = to.z; kid.path.shift(); } else p.position.addScaledVector(d.normalize(), v);
  p.position.y = lerp(p.position.y, to.y, 0.25);
  pose(p, kid.stage === 'class' ? 'walk' : 'run', t + hash(kid.id) % 10);
  text(kid.fx, '');
}

function step(kid, dt, t, now) {
  const p = kid.p, c = kid.cls, k = kid.data;
  if (kid.handUntil) { // hand up before leaving
    if (now < kid.handUntil) { pose(p, 'hand', t); text(kid.fx, '🙋'); return; }
    kid.handUntil = 0;
    const skip = seatSkip(kid);
    c.seats.delete(kid.id); kid.seat = -1; kid.at = '';
    kid.stage = 'toPark';
    go(kid, classNav, DOOR_IN, skip, DOOR_OUT.clone());
  }
  if (kid.path.length) { walk(kid, dt, t); return; }
  switch (kid.stage) {
    case 'toPark': // out of the room: along the hallway and across the plaza to the park
      park.attach(p);
      if (k.running) { kid.stage = 'toClass'; kid.path = [V3(c.x + DOOR_X, 0.06, HALL_Z)]; return; }
      kid.stage = 'park';
      assign(kid);
      enterPark(kid, [V3(0, 0.06, HALL_Z), GATE.clone()]);
      return;
    case 'toClass':
      c.g.attach(p);
      kid.stage = 'class';
      takeSeat(kid);
      kid.path = [DOOR_IN.clone(), ...classNav.path(DOOR_IN, seatOf(kid), seatSkip(kid))];
      kid.skip = seatSkip(kid);
      return;
    case 'park': parkStep(kid, dt, t); return;
  }
  if (kid.at) { // in a corner of the room, busy
    face(p, c.face[kid.at], 0.15);
    pose(p, 'work', t + hash(kid.id) % 7);
    text(kid.fx, { books: '📖', computers: '⌨️', lab: '🧪', web: '🌍', teacher: '🗣️' }[kid.at] || '');
    return;
  }
  face(p, Math.PI);
  const thinking = !k.tool || Date.now() - new Date(k.modified) > 45e3;
  pose(p, thinking ? 'think' : 'sit', t + hash(kid.id) % 5);
  text(kid.fx, thinking ? '💭' : '✏️');
}

// the football: passes between whoever is on the pitch; the ball flies to
// where the one it goes to is
function playBall(dt) {
  const players = [...actors.values()].filter(a => a.play === 'football' && a.stage === 'park' && !a.path.length);
  if (!players.includes(game.holder)) game.holder = null;
  game.t += dt * 0.9;
  if (game.t >= 1) {
    game.t = 0;
    game.from.copy(ball.position).setY(0.2);
    game.kicker = game.holder;
    const others = players.filter(p => p !== game.holder);
    game.holder = others.length ? others[Math.floor(Math.random() * others.length)] : players[0] || null; // alone, it juggles
  }
  if (game.holder) game.to.copy(game.holder.p.position).setY(0.2); else game.to.set(PITCH.x, 0.2, PITCH.z);
  ball.position.lerpVectors(game.from, game.to, game.t);
  const alone = players.length === 1;
  ball.position.y = 0.2 + (game.holder ? Math.sin(game.t * Math.PI) * (alone ? 1.6 : 1.1) : 0);
  ball.rotation.x += dt * (game.holder ? 8 : 0);
}

// ---------- the teacher's day ----------
const HOME = V3(0, 0.1, -D / 2 + 1.9);
const ROUTINE = ['coffee', 'board', 'apple', 'window', 'stroll', 'plant'];
// teacherGo walks her round her desk and the kids' desks
function teacherGo(c, to) { c.routine.path = classNav.path(c.teacher.position, to, classNav.bit('tchair')); }
function teach(c, dt, t, now) {
  const cls = c.data, p = c.teacher, r = c.routine;
  const lesson = cls.lesson || cls.title || '';
  const n = cls.state === 'working' ? Math.min(lesson.length, Math.floor(c.chalk)) : lesson.length;
  if (cls.state === 'working') c.chalk += dt * 14;
  if (c.b.userData.text !== cls.title + '\n' + lesson || c.b.userData.shown !== n) {
    c.b.userData.text = cls.title + '\n' + lesson;
    c.b.userData.shown = n;
    drawBoard(c.b, cls.title || '', lesson, n);
  }
  c.bubble.visible = cls.state === 'blocked';
  // what she does: works the board, calls you, or goes through her routine
  let want = cls.state === 'working' ? 'teach' : cls.state === 'blocked' ? 'call' : '';
  if (!want) {
    if (!ROUTINE.includes(r.kind) || now > r.until) {
      r.i = (r.i + 1) % ROUTINE.length;
      r.until = now + 9000 + (hash(c.id + r.i) % 6000);
      r.kind = '';
    }
    want = ROUTINE[r.i];
  }
  if (r.kind !== want) {
    r.kind = want;
    const spot = {
      teach: V3(0, 0.1, -D / 2 + 1.0), call: HOME, coffee: HOME, apple: HOME,
      board: new THREE.Vector3(-1.5 + (hash(c.id + now) % 30) / 10, 0.1, -D / 2 + 0.75),
      window: new THREE.Vector3(-W / 2 + 1.3, 0.1, -3), plant: new THREE.Vector3(-W / 2 + 1.4, 0.1, -D / 2 + 1.3),
      stroll: (() => { const used = [...c.seats.values()]; const s = seatPos(c, used.length ? used[hash(now) % used.length] : 5); return V3(s.x + 1.35, 0.1, s.z); })(), // in the aisle by a kid
    }[want];
    teacherGo(c, spot);
  }
  const holding = { coffee: 'mug', apple: 'apple' }[r.kind];
  c.inHand.mug.visible = holding === 'mug' && !r.path.length;
  c.inHand.apple.visible = holding === 'apple' && !r.path.length;
  c.mug.visible = !c.inHand.mug.visible;
  c.apple.visible = !c.inHand.apple.visible;
  if (r.kind === 'apple' && !r.path.length) c.inHand.apple.scale.setScalar(Math.max(0.4, 1 - ((now % 9000) / 9000) * 0.6));
  text(c.doing, { coffee: '☕', apple: '🍎', board: '✏️', window: '🌤️', plant: '💧', stroll: '👀', call: '❗', teach: '' }[r.kind] || '');
  if (r.path.length) { // walking
    const to = r.path[0], d = to.clone().sub(p.position); d.y = 0;
    const dist = d.length(), v = 1.3 * dt;
    if (dist > 0.01) p.rotation.y = lerpAngle(p.rotation.y, Math.atan2(d.x, d.z), 0.2);
    if (dist <= v) { p.position.copy(to); r.path.shift(); } else p.position.addScaledVector(d.normalize(), v);
    pose(p, 'walk', t, 0.7);
    return;
  }
  switch (r.kind) {
    case 'teach': { // paces along the board and writes on it
      c.pace += dt;
      const x = Math.sin(c.pace * 0.35) * 2.6, writing = Math.sin(c.pace * 0.7) > 0.2;
      const to = V3(x, 0.1, -D / 2 + (writing ? 0.75 : 1.15)), d = to.clone().sub(p.position);
      p.position.lerp(to, 0.04);
      if (writing && d.length() < 0.3) { p.rotation.y = lerpAngle(p.rotation.y, Math.PI, 0.15); pose(p, 'write', t); }
      else { p.rotation.y = lerpAngle(p.rotation.y, Math.atan2(d.x, d.z), 0.1); pose(p, d.length() > 0.08 ? 'walk' : 'idle', t, 0.7); }
      return;
    }
    case 'call': p.rotation.y = lerpAngle(p.rotation.y, 0, 0.15); pose(p, 'wave', t); return;
    case 'coffee': p.rotation.y = lerpAngle(p.rotation.y, 0, 0.15); pose(p, 'sip', t); return;
    case 'apple': p.rotation.y = lerpAngle(p.rotation.y, 0, 0.15); pose(p, 'eat', t); return;
    case 'board': p.rotation.y = lerpAngle(p.rotation.y, Math.PI, 0.15); pose(p, 'write', t); return;
    case 'window': p.rotation.y = lerpAngle(p.rotation.y, -Math.PI / 2, 0.1); pose(p, 'look', t); return;
    case 'plant': p.rotation.y = lerpAngle(p.rotation.y, -Math.PI * 0.75, 0.1); pose(p, 'water', t); return;
    case 'stroll': p.rotation.y = lerpAngle(p.rotation.y, -Math.PI / 2, 0.1); pose(p, 'look', t); return;
  }
}

// ---------- snapshots from nanny ----------
function sync(state) {
  words = state.words || {};
  document.body.style.fontFamily = state.font || '';
  const now = performance.now();
  const seen = new Set();
  arrivals = 0;
  let playing = 0;
  const finishedAll = [];
  for (const cls of state.classes || []) for (const k of cls.kids || []) if (!k.running) finishedAll.push(k);
  finishedAll.sort((a, b) => new Date(b.modified) - new Date(a.modified));
  const inPark = new Set(finishedAll.slice(0, PARK_MAX).map(k => k.id));
  for (const cls of state.classes || []) {
    seen.add(cls.id);
    let c = classes.get(cls.id);
    if (!c) { c = buildRoom(cls); classes.set(cls.id, c); order.push(cls.id); }
    if (c.data.lesson !== cls.lesson) c.chalk = 0;
    c.data = cls;
    c.tl.element.textContent = (cls.state === 'blocked' ? '❗ ' : cls.state === 'working' ? '✎ ' : cls.state === 'done' ? '◆ ' : '') + short(cls.title, 40);
    c.bubble.element.textContent = '⚠ ' + T('waiting for you') + (cls.prompt ? ': ' + short(cls.prompt, 120) : '');
    c.lamp.color.set({ working: 0xffd28a, blocked: 0xff4d4d, done: 0x8ab8ff }[cls.state] || 0xffe9c8);
    const kids = cls.kids || [];
    const show = new Set(kids.filter(k => k.running || inPark.has(k.id)).map(k => k.id));
    for (const k of kids) {
      if (!show.has(k.id)) continue;
      if (!k.running) playing++;
      let kid = actors.get(k.id);
      if (!kid) kid = newKid(c, k, !first);
      kid.data = k;
      kid.l.element.textContent = k.type + ' · ' + short(k.desc, 36);
      plan(kid, now);
    }
    for (const kid of [...actors.values()]) if (kid.cls === c && !show.has(kid.id)) drop(kid);
  }
  for (const id of [...classes.keys()]) if (!seen.has(id)) dropClass(id);
  for (const c of classes.values()) {
    const used = new Set(c.seats.values());
    c.desks.forEach((d, i) => { d.visible = i < Math.max(4, Math.ceil((Math.max(...used, -1) + 1) / COLS) * COLS); });
  }
  parkSign.element.textContent = `🛝 ${T('recess')} · ${playing} ${T('playing')}`;
  layout();
  hud(state);
  first = false;
  if (selected) card();
}
function drop(kid) {
  kid.cls.seats.delete(kid.id);
  kid.p.parent?.remove(kid.p);
  kid.p.traverse(o => o.element && o.element.remove());
  actors.delete(kid.id);
  if (game.holder === kid) game.holder = null;
  if (game.kicker === kid) game.kicker = null;
  if (chase.it === kid) chase.it = null;
  if (selected === kid) select(null);
}
function dropClass(id) {
  const c = classes.get(id);
  for (const kid of [...actors.values()]) if (kid.cls === c) drop(kid);
  c.g.traverse(o => o.element && o.element.remove());
  scene.remove(c.g);
  classes.delete(id);
  order = order.filter(x => x !== id);
  if (selected && selected.teacher === c) select(null);
}

// layout puts the rooms side by side under one roof, the hallway in front.
let span = W + GAP;
function layout() {
  const n = Math.max(order.length, 1);
  span = n * (W + GAP);
  order.forEach((id, i) => {
    const c = classes.get(id);
    c.x = (i - (n - 1) / 2) * (W + GAP);
    if (!c.placed) { c.g.position.x = c.x; c.placed = true; }
  });
  hall.scale.x = span + GAP;
  hall.position.set(0, 0.03, HALL_Z);
  plaza.scale.x = 8;
  plaza.position.set(0, 0.02, (HALL_Z + 1.5 + PARK.z0) / 2);
  // the school's back: a tall facade with a sign and a roof behind the rooms
  if (school.userData.n !== n) {
    school.userData.n = n;
    school.clear();
    const w = span + 2;
    school.add(at(box(w, 6, 1.2, 0xe8d8bf), 0, 3, -D / 2 - 0.9));
    school.add(at(box(w, 0.4, 1.4, 0xb5482e), 0, 6.1, -D / 2 - 0.9));
    const roof = box(w, 0.3, 8, 0xb5482e); roof.rotation.x = 0.35;
    school.add(at(roof, 0, 4.6, -D / 2 - 5));
    school.add(at(box(w, 4, 7, 0xe8d8bf), 0, 2, -D / 2 - 5));
    const sign = new THREE.Mesh(new THREE.PlaneGeometry(9, 1.4), new THREE.MeshStandardMaterial({ map: signTex('nanny school'), roughness: 0.6 }));
    school.add(at(sign, 0, 4.9, -D / 2 - 0.28));
    for (const s of [-1, 1]) school.add(at(cyl(0.3, 0.3, 6.3, 0xffffff), s * (w / 2), 3.15, -D / 2 - 0.3));
    const flag = new THREE.Group();
    flag.add(at(cyl(0.05, 0.05, 4, 0xcccccc, 8), 0, 2, 0));
    flag.add(at(box(1.2, 0.7, 0.03, 0x7c5cff), 0.6, 3.6, 0));
    school.add(at(flag, w / 2 + 2, 6, -D / 2 - 1));
  }
  const s = sun.shadow.camera, r = Math.max(span / 2 + 12, 40);
  s.left = -r; s.right = r; s.top = r; s.bottom = -r; s.near = 1; s.far = 260;
  s.updateProjectionMatrix();
  sun.position.set(30, 60, 50);
  sun.target.position.set(0, 0, 12);
}

// ---------- HUD, picking, the card ----------
const top = document.getElementById('top'), cardEl = document.getElementById('card');
let focusId = null, selected = null, follow = false;
function hud(state) {
  document.getElementById('help').textContent = T('click a kid or a teacher · drag to look around · scroll to zoom');
  const empty = document.getElementById('empty');
  empty.style.display = (state.classes || []).length ? 'none' : 'grid';
  empty.firstElementChild.textContent = '🏫 ' + T('No agents running right now') + ' · ' + T('start one in nanny and it walks in');
  const want = ['all', ...order, 'park'];
  const have = [...top.querySelectorAll('.chip')].map(b => b.dataset.id);
  if (want.join() !== have.join()) {
    top.querySelectorAll('.chip').forEach(b => b.remove());
    for (const id of want) {
      const b = document.createElement('button');
      b.className = 'chip'; b.dataset.id = id;
      b.innerHTML = '<i class="dot"></i><span></span>';
      b.onclick = () => fly(id === 'all' ? null : id);
      top.appendChild(b);
    }
  }
  for (const b of top.querySelectorAll('.chip')) {
    const c = classes.get(b.dataset.id);
    b.querySelector('span').textContent = c ? short(c.data.project + ' · ' + c.data.title, 34) : b.dataset.id === 'park' ? '🛝 ' + T('recess') : T('all classes');
    b.querySelector('.dot').className = 'dot ' + (c ? c.data.state : '');
    b.querySelector('.dot').style.display = c ? '' : 'none';
    b.classList.toggle('on', (b.dataset.id === 'all' && !focusId) || b.dataset.id === focusId);
  }
}
const camGoal = { pos: null, target: null };
function fly(id) {
  focusId = id;
  follow = false;
  if (!id) {
    camGoal.target = new THREE.Vector3(0, 0.8, 14);
    camGoal.pos = new THREE.Vector3(0, 22 + span * 0.25, 52 + span * 0.3);
  } else if (id === 'park') {
    camGoal.target = new THREE.Vector3(0, 0.5, 24);
    camGoal.pos = new THREE.Vector3(6, 22, 52);
  } else {
    const c = classes.get(id);
    camGoal.target = new THREE.Vector3(c.x, 0.8, 0.5);
    camGoal.pos = new THREE.Vector3(c.x + 3, 11, D / 2 + 13);
  }
  hud({ classes: order });
}
const ray = new THREE.Raycaster(), mouse = new THREE.Vector2();
let downAt = null;
renderer.domElement.addEventListener('pointerdown', e => { downAt = [e.clientX, e.clientY]; });
renderer.domElement.addEventListener('pointerup', e => {
  if (!downAt || Math.hypot(e.clientX - downAt[0], e.clientY - downAt[1]) > 5) return; // a drag
  const hit = hitTest(e);
  if (!hit) return select(null);
  const { kind, id } = hit.userData;
  select(kind === 'teacher' ? { teacher: classes.get(id) } : actors.get(id));
});
renderer.domElement.addEventListener('pointermove', e => {
  const hit = hitTest(e);
  renderer.domElement.style.cursor = hit ? 'pointer' : '';
  for (const kid of actors.values()) kid.l.element.classList.toggle('hidden', !(hit === kid.p || selected === kid));
});
function hitTest(e) {
  mouse.set((e.clientX / innerWidth) * 2 - 1, -(e.clientY / innerHeight) * 2 + 1);
  ray.setFromCamera(mouse, camera);
  const people = [...actors.values()].map(a => a.p).concat([...classes.values()].map(c => c.teacher));
  const h = ray.intersectObjects(people, true)[0];
  return h ? h.object.userData.actor : null;
}
function select(s) {
  selected = s;
  for (const kid of actors.values()) kid.l.element.classList.toggle('hidden', kid !== s);
  card();
}
function card() {
  if (!selected) { cardEl.style.display = 'none'; return; }
  cardEl.style.display = 'block';
  const esc = s => String(s ?? '').replace(/[&<>"]/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[ch]);
  const kv = rows => '<div class="kv">' + rows.filter(r => r[1]).map(([k, v]) => `<b>${esc(k)}</b><span>${esc(v)}</span>`).join('') + '</div>';
  let html = '<button class="x" title="close">×</button>';
  if (selected.teacher) {
    const d = selected.teacher.data;
    const st = { working: T('working'), blocked: T('needs you'), done: T('done') }[d.state] || T('idle');
    html += `<h3>👩‍🏫 ${esc(d.title)}</h3><div>${esc(d.project)}${d.branch ? ' · ⎇ ' + esc(d.branch) : ''} · <b>${esc(st)}</b></div>`;
    html += kv([[T('context'), d.context ? d.context + '%' : ''], ['$', d.cost ? d.cost.toFixed(2) : ''], ['nanny', d.where]]);
    if (d.prompt) html += `<div class="say" style="border:2px solid var(--red)">⚠ ${esc(d.prompt)}</div>`;
    if (d.lesson) html += `<div class="say"><b>${esc(T('Lesson'))}</b>\n${esc(short(d.lesson, 600))}</div>`;
  } else {
    const k = selected.data;
    html += `<h3>🧒 ${esc(k.type)}</h3><div>${esc(k.desc)}</div>`;
    html += kv([['', k.running ? T('running') : T('finished')], [T('last tool'), k.tool], [T('started'), ago(k.started)], [T('last act.'), ago(k.modified)]]);
    if (k.text) html += `<div class="say">${esc(short(k.text, 500))}</div>`;
  }
  html += `<button class="chip follow">${follow ? '✓ ' : ''}🎥 ${esc(T('follow'))}</button>`;
  cardEl.innerHTML = html;
  cardEl.querySelector('.x').onclick = () => select(null);
  cardEl.querySelector('.follow').onclick = () => { follow = !follow; card(); };
}

// ---------- the loop ----------
const clock = new THREE.Clock();
let lastSky = 0;
function frame() {
  const dt = Math.min(clock.getDelta(), 0.05), t = clock.elapsedTime, now = performance.now();
  if (now - lastSky > 60e3) { lightUp(); lastSky = now; }
  const busyBy = new Map();
  for (const a of actors.values()) { // who is busy at what, for the computers, the lab and the globe
    if (a.stage === 'class' && a.at && !a.path.length) { if (!busyBy.has(a.cls)) busyBy.set(a.cls, new Set()); busyBy.get(a.cls).add(a.at); }
  }
  for (const c of classes.values()) {
    c.g.position.x = lerp(c.g.position.x, c.x, 0.08);
    teach(c, dt, t, now);
    c.hand.rotation.z = -t * 0.2;
    c.lamp.intensity = (night() ? 14 : 4) * (c.data.state === 'blocked' ? 0.6 + 0.4 * Math.abs(Math.sin(t * 3)) : 1);
    const busy = busyBy.get(c) || new Set();
    c.pcs.userData.screens.forEach((s, i) => { s.material.emissiveIntensity = busy.has('computers') ? 0.8 + Math.sin(t * 20 + i) * 0.3 : 0.15; });
    c.bench.userData.liquids.forEach((l, i) => { l.material.emissiveIntensity = busy.has('lab') ? 0.6 + Math.sin(t * 8 + i) * 0.4 : 0.2; l.position.y = busy.has('lab') ? Math.sin(t * 9 + i) * 0.015 : 0; });
    c.glb.userData.ball.rotation.y += dt * (busy.has('web') ? 2.5 : 0.1);
  }
  for (const kid of actors.values()) step(kid, dt, t, now);
  tagGame(dt, t);
  props(dt, t);
  playBall(dt);
  jet.scale.y = 1 + Math.sin(t * 6) * 0.08;
  water.rotation.y += dt * 0.3;
  for (const car of cars) { car.g.position.x += car.dir * car.speed * dt; if (Math.abs(car.g.position.x) > 160) car.g.position.x = -car.dir * 160; }
  for (const cl of clouds) { cl.position.x += dt * 1.2; if (cl.position.x > 180) cl.position.x = -180; }
  for (let i = notes.length - 1; i >= 0; i--) {
    const n = notes[i];
    n.t += dt * 0.6;
    n.n.position.lerpVectors(n.from, n.to, Math.min(n.t, 1));
    n.n.position.y += Math.sin(Math.min(n.t, 1) * Math.PI) * 1.5;
    if (n.t >= 1.3) { n.c.g.remove(n.n); n.n.element.remove(); notes.splice(i, 1); }
  }
  if (camGoal.pos) {
    camera.position.lerp(camGoal.pos, 0.06);
    controls.target.lerp(camGoal.target, 0.08);
    if (camera.position.distanceTo(camGoal.pos) < 0.1) camGoal.pos = null;
  }
  if (follow && selected) {
    const p = (selected.teacher ? selected.teacher.teacher : selected.p).getWorldPosition(new THREE.Vector3());
    controls.target.lerp(p.setY(0.9), 0.1);
  }
  if (now - lastInput > 30e3 && !follow) controls.autoRotate = true;
  controls.update();
  renderer.render(scene, camera);
  labels.render(scene, camera);
  requestAnimationFrame(frame);
}

async function poll() {
  try {
    const r = await fetch('state.json', { cache: 'no-store' });
    if (!r.ok) throw new Error(r.status);
    const state = await r.json();
    document.getElementById('gone').style.display = 'none';
    const wasEmpty = !order.length;
    sync(state);
    if (wasEmpty && order.length) fly(order.length === 1 ? order[0] : null);
  } catch {
    document.getElementById('gone').style.display = 'grid';
  }
  setTimeout(poll, 1000);
}
lightUp();
layout();
poll();
frame();
