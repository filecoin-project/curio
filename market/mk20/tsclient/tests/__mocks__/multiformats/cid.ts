export class CID {
  public code: number;
  public multihash: any;

  constructor(code: number, multihash: any) {
    this.code = code;
    this.multihash = multihash;
  }

  static create(version: number, code: number, multihash: any): CID {
    return new CID(code, multihash);
  }

  toString(): string {
    return `bafybeihq6mbsd757cdm4sn6z5r7w6tdvkrb3q9iu3pjr7q3ip24c65qh2i`;
  }
}
