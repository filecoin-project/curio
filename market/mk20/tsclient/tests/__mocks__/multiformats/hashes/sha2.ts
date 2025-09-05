export const sha256 = {
  async digest(data: Uint8Array): Promise<any> {
    return {
      code: 0x12,
      digest: new Uint8Array(32).fill(1)
    };
  }
};
