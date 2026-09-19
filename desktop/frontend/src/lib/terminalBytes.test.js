import { describe, it, expect } from 'vitest';
import { decodeBase64ToBytes } from './terminalBytes';

describe('decodeBase64ToBytes', () => {
  it('decodes ASCII output to its bytes', () => {
    // base64 of "hi"
    expect(Array.from(decodeBase64ToBytes('aGk='))).toEqual([104, 105]);
  });

  it('preserves non-UTF-8 control bytes', () => {
    // base64 of bytes [0x1b, 0x5b, 0x00, 0xff] — an escape sequence plus binary
    expect(Array.from(decodeBase64ToBytes('G1sA/w=='))).toEqual([27, 91, 0, 255]);
  });

  it('decodes empty output to an empty array', () => {
    expect(decodeBase64ToBytes('').length).toBe(0);
  });
});
