// Небольшой генератор QR-кода (байтовый режим, коррекция M, версии 1–10): без внешних зависимостей.
// Алгоритм стандартный (ISO/IEC 18004): RS-коды над GF(256), восемь масок, выбор по штрафу.

const ECC_PER_BLOCK_M = [10, 16, 26, 18, 24, 16, 18, 22, 22, 26];
const BLOCKS_M = [1, 1, 1, 2, 2, 4, 4, 4, 5, 5];
export const MAX_VERSION = 10;

function rawModules(v: number): number {
  let r = (16 * v + 128) * v + 64;
  if (v >= 2) {
    const n = Math.floor(v / 7) + 2;
    r -= (25 * n - 10) * n - 55;
    if (v >= 7) r -= 36;
  }
  return r;
}

function dataCodewords(v: number): number {
  return Math.floor(rawModules(v) / 8) - ECC_PER_BLOCK_M[v - 1] * BLOCKS_M[v - 1];
}

/** Сколько байт текста помещается в QR максимальной поддерживаемой версии. */
export const MAX_BYTES = dataCodewords(MAX_VERSION) - 3;

function gfMul(x: number, y: number): number {
  let z = 0;
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d);
    z ^= ((y >>> i) & 1) * x;
  }
  return z;
}

function rsDivisor(degree: number): number[] {
  const res = new Array<number>(degree).fill(0);
  res[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < res.length; j++) {
      res[j] = gfMul(res[j], root);
      if (j + 1 < res.length) res[j] ^= res[j + 1];
    }
    root = gfMul(root, 2);
  }
  return res;
}

function rsRemainder(data: number[], divisor: number[]): number[] {
  const res = new Array<number>(divisor.length).fill(0);
  for (const b of data) {
    const f = b ^ (res.shift() as number);
    res.push(0);
    divisor.forEach((c, i) => { res[i] ^= gfMul(c, f); });
  }
  return res;
}

function utf8(text: string): number[] {
  return Array.from(new TextEncoder().encode(text));
}


/** Матрица модулей: true — тёмный. Бросает ошибку, если текст не помещается. */
export function qrMatrix(text: string): boolean[][] {
  const bytes = utf8(text);
  let version = 1;
  while (version <= MAX_VERSION && bytes.length > dataCodewords(version) - 2 - (version >= 10 ? 1 : 0)) version++;
  if (version > MAX_VERSION) throw new Error("Текст слишком длинный для QR");

  const size = version * 4 + 17;
  const modules: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const isFn: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const setFn = (x: number, y: number, dark: boolean) => {
    if (x < 0 || y < 0 || x >= size || y >= size) return;
    modules[y][x] = dark;
    isFn[y][x] = true;
  };

  // Служебные узоры
  for (let i = 0; i < size; i++) {
    setFn(6, i, i % 2 === 0);
    setFn(i, 6, i % 2 === 0);
  }
  const finder = (cx: number, cy: number) => {
    for (let dy = -4; dy <= 4; dy++) for (let dx = -4; dx <= 4; dx++) {
      const d = Math.max(Math.abs(dx), Math.abs(dy));
      setFn(cx + dx, cy + dy, d !== 2 && d !== 4);
    }
  };
  finder(3, 3); finder(size - 4, 3); finder(3, size - 4);
  if (version >= 2) {
    const n = Math.floor(version / 7) + 2;
    const step = version === 32 ? 26 : Math.ceil((version * 4 + 4) / (n * 2 - 2)) * 2;
    const pos = [6];
    for (let p = size - 7; pos.length < n; p -= step) pos.splice(1, 0, p);
    pos.forEach((ax, i) => pos.forEach((ay, j) => {
      if ((i === 0 && j === 0) || (i === 0 && j === n - 1) || (i === n - 1 && j === 0)) return;
      for (let dy = -2; dy <= 2; dy++) for (let dx = -2; dx <= 2; dx++) setFn(ax + dx, ay + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    }));
  }
  const drawFormat = (mask: number) => {
    const data = (0 << 3) | mask; // уровень M = 00
    let rem = data;
    for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
    const bits = ((data << 10) | rem) ^ 0x5412;
    const bit = (i: number) => ((bits >>> i) & 1) !== 0;
    for (let i = 0; i <= 5; i++) setFn(8, i, bit(i));
    setFn(8, 7, bit(6)); setFn(8, 8, bit(7)); setFn(7, 8, bit(8));
    for (let i = 9; i < 15; i++) setFn(14 - i, 8, bit(i));
    for (let i = 0; i < 8; i++) setFn(size - 1 - i, 8, bit(i));
    for (let i = 8; i < 15; i++) setFn(8, size - 15 + i, bit(i));
    setFn(8, size - 8, true);
  };
  drawFormat(0);
  if (version >= 7) {
    let rem = version;
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
    const bits = (version << 12) | rem;
    for (let i = 0; i < 18; i++) {
      const dark = ((bits >>> i) & 1) !== 0;
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      setFn(a, b, dark);
      setFn(b, a, dark);
    }
  }

  // Данные: режим 0100, длина, байты, терминатор, добивка
  const cap = dataCodewords(version);
  const bitsArr: number[] = [];
  const put = (val: number, len: number) => { for (let i = len - 1; i >= 0; i--) bitsArr.push((val >>> i) & 1); };
  put(4, 4);
  put(bytes.length, version >= 10 ? 16 : 8);
  bytes.forEach((b) => put(b, 8));
  put(0, Math.min(4, cap * 8 - bitsArr.length));
  while (bitsArr.length % 8) bitsArr.push(0);
  for (let pad = 0xec; bitsArr.length < cap * 8; pad ^= 0xec ^ 0x11) put(pad, 8);
  const data: number[] = [];
  for (let i = 0; i < bitsArr.length; i += 8) data.push(parseInt(bitsArr.slice(i, i + 8).join(""), 2));

  // Блоки и коррекция ошибок
  const nBlocks = BLOCKS_M[version - 1];
  const eccLen = ECC_PER_BLOCK_M[version - 1];
  const raw = Math.floor(rawModules(version) / 8);
  const shortLen = Math.floor(raw / nBlocks);
  const nShort = nBlocks - (raw % nBlocks);
  const div = rsDivisor(eccLen);
  const blocks: number[][] = [];
  const eccs: number[][] = [];
  for (let i = 0, k = 0; i < nBlocks; i++) {
    const len = shortLen - eccLen + (i < nShort ? 0 : 1);
    const dat = data.slice(k, k + len);
    k += len;
    blocks.push(dat);
    eccs.push(rsRemainder(dat, div));
  }
  const all: number[] = [];
  for (let i = 0; i < blocks[nBlocks - 1].length; i++) blocks.forEach((b) => { if (i < b.length) all.push(b[i]); });
  for (let i = 0; i < eccLen; i++) eccs.forEach((e) => all.push(e[i]));

  // Размещение
  let bi = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const x = right - j;
        const upward = ((right + 1) & 2) === 0;
        const y = upward ? size - 1 - vert : vert;
        if (!isFn[y][x] && bi < all.length * 8) {
          modules[y][x] = ((all[bi >>> 3] >>> (7 - (bi & 7))) & 1) !== 0;
          bi++;
        }
      }
    }
  }

  // Маска с минимальным штрафом
  const maskFn = [
    (x: number, y: number) => (x + y) % 2 === 0,
    (_x: number, y: number) => y % 2 === 0,
    (x: number) => x % 3 === 0,
    (x: number, y: number) => (x + y) % 3 === 0,
    (x: number, y: number) => (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0,
    (x: number, y: number) => ((x * y) % 2) + ((x * y) % 3) === 0,
    (x: number, y: number) => (((x * y) % 2) + ((x * y) % 3)) % 2 === 0,
    (x: number, y: number) => (((x + y) % 2) + ((x * y) % 3)) % 2 === 0,
  ];
  const apply = (m: number) => {
    for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) if (!isFn[y][x] && maskFn[m](x, y)) modules[y][x] = !modules[y][x];
  };
  let best = 0;
  let bestPenalty = Infinity;
  for (let m = 0; m < 8; m++) {
    apply(m);
    drawFormat(m);
    const p = penalty(modules);
    if (p < bestPenalty) { bestPenalty = p; best = m; }
    apply(m);
  }
  apply(best);
  drawFormat(best);
  return modules;
}

function penalty(m: boolean[][]): number {
  const n = m.length;
  let p = 0;
  const runs = (get: (a: number, b: number) => boolean) => {
    for (let a = 0; a < n; a++) {
      let run = 1;
      for (let b = 1; b < n; b++) {
        if (get(a, b) === get(a, b - 1)) { run++; if (run === 5) p += 3; else if (run > 5) p++; } else run = 1;
      }
    }
  };
  runs((a, b) => m[a][b]);
  runs((a, b) => m[b][a]);
  for (let y = 0; y < n - 1; y++) for (let x = 0; x < n - 1; x++) {
    if (m[y][x] === m[y][x + 1] && m[y][x] === m[y + 1][x] && m[y][x] === m[y + 1][x + 1]) p += 3;
  }
  const pat = [1, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0];
  const match = (get: (i: number) => boolean, at: number, rev: boolean) =>
    pat.every((v, k) => (get(at + k) ? 1 : 0) === (rev ? pat[10 - k] : v));
  for (let a = 0; a < n; a++) for (let b = 0; b <= n - 11; b++) for (const rev of [false, true]) {
    if (match((i) => m[a][i], b, rev)) p += 40;
    if (match((i) => m[i][a], b, rev)) p += 40;
  }
  const dark = m.flat().filter(Boolean).length;
  const k = Math.ceil(Math.abs(dark * 20 - n * n * 10) / (n * n)) - 1;
  return p + Math.max(0, k) * 10;
}

/** SVG-строка QR с тихой зоной в 4 модуля; shape-rendering отключает сглаживание для печати. */
export function qrSvg(text: string, quiet = 4): string {
  const m = qrMatrix(text);
  const dim = m.length + quiet * 2;
  let d = "";
  m.forEach((row, y) => row.forEach((dark, x) => { if (dark) d += `M${x + quiet},${y + quiet}h1v1h-1z`; }));
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${dim} ${dim}" shape-rendering="crispEdges"><rect width="${dim}" height="${dim}" fill="#fff"/><path d="${d}" fill="#000"/></svg>`;
}
