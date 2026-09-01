const seq = '0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')

const RandomUtil = {
  randomIntRange(min: number, max: number): number {
    if (!Number.isSafeInteger(min)){
      return this.randomIntRange(Number.MIN_SAFE_INTEGER, max)
    }
    if (!Number.isSafeInteger(max)){
      return this.randomIntRange(min, Number.MAX_SAFE_INTEGER)
    }
    if (max < min) {
      return this.randomIntRange(max, min)
    }
    const array = new Uint32Array(2);
    window.crypto.getRandomValues(array);
    const highbits = array[0]
    const lowbits = array[1] >>> 11
    const random = (highbits * 2 ** 21 + lowbits) / (Number.MAX_SAFE_INTEGER + 1)
    return Math.floor(random * (max - min + 1) + min)
  },
  randomInt(n: number) {
    // exclusive upper bound: callers index arrays with the result
    return this.randomIntRange(0, n - 1)
  },
  randomSeq(count: number): string {
    if (count <= 0) {
      return ''
    }
    let str = ''
    for (let i = 0; i < count; ++i) {
        str += seq[this.randomInt(seq.length)]
    }
    return str
  },
  randomLowerAndNum(count: number): string {
    if (count <= 0) {
      return ''
    }
    let str = ''
    for (let i = 0; i < count; ++i) {
        str += seq[this.randomInt(36)]
    }
    return str
  },
  randomUUID(): string {
    const rng = new Uint8Array(16);
    window.crypto.getRandomValues(rng);
    rng[6] = (rng[6] & 0x0f) | 0x40;
    rng[8] = (rng[8] & 0x3f) | 0x80;
    return (
      byteToHex[rng[0]] + byteToHex[rng[1]] + byteToHex[rng[2]] + byteToHex[rng[3]] + '-' +
      byteToHex[rng[4]] + byteToHex[rng[5]] + '-' +
      byteToHex[rng[6]] + byteToHex[rng[7]] + '-' +
      byteToHex[rng[8]] + byteToHex[rng[9]] + '-' +
      byteToHex[rng[10]] + byteToHex[rng[11]] + byteToHex[rng[12]] +
      byteToHex[rng[13]] + byteToHex[rng[14]] + byteToHex[rng[15]]
    );
  },
  randomShadowsocksPassword(n: number): string {
    const array = new Uint8Array(n)
    window.crypto.getRandomValues(array)
    return btoa(String.fromCharCode(...array))
  },
  randomShortId(): string[] {
    const shortIds = new Array(24).fill('')
    for (let ii = 1; ii < 24; ii++) {
      for (let jj = 0; jj <= this.randomInt(8); jj++){
          const randomNum = this.randomInt(256)
          shortIds[ii] += ('0' + randomNum.toString(16)).slice(-2)
      }
  }
  return shortIds
  }
}

const byteToHex = Array.from(
  { length: 256 },
  (_, i) => (i + 0x100)
    .toString(16)
    .slice(1)
)

export default RandomUtil