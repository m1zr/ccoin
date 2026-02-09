// Transaction building and signing
import {
  WalletKeys,
  Note,
  Hash,
  Address,
  createCommitment,
  deriveNullifier,
  selectNotesForSpending,
  createOutputNotes,
  encryptNote,
  bytesToHex,
  bigintToBytes,
  randomBytes,
} from './crypto';
import { generateProof, ProofData } from './zkp';

/**
 * Transaction output
 */
export interface TxOutput {
  address: Address;
  value: bigint;
  memo?: Uint8Array;
}

/**
 * Disclosure for transparency
 */
export interface Disclosure {
  type: 'range' | 'identity' | 'temporal' | 'sanctions';
  proof: Uint8Array;
  publicData: Record<string, unknown>;
}

/**
 * Transaction request
 */
export interface TransactionRequest {
  outputs: TxOutput[];
  fee: bigint;
  disclosures?: Disclosure[];
  memo?: Uint8Array;
}

/**
 * Built transaction ready for submission
 */
export interface Transaction {
  version: number;
  nullifiers: Hash[];
  commitments: Hash[];
  encryptedOutputs: Uint8Array[];
  proof: Uint8Array;
  anchor: Hash;
  fee: bigint;
  disclosureFlags: number;
  disclosures: Disclosure[];
  memo?: Uint8Array;
}

/**
 * Transaction builder
 */
export class TransactionBuilder {
  private inputs: Note[] = [];
  private outputs: TxOutput[] = [];
  private fee: bigint = BigInt(0);
  private disclosures: Disclosure[] = [];
  private memo?: Uint8Array;

  constructor(
    private keys: WalletKeys,
    private availableNotes: Note[],
    private anchor: Hash
  ) {}

  /**
   * Add outputs to send
   */
  addOutputs(outputs: TxOutput[]): this {
    this.outputs.push(...outputs);
    return this;
  }

  /**
   * Set transaction fee
   */
  setFee(fee: bigint): this {
    this.fee = fee;
    return this;
  }

  /**
   * Add a disclosure proof
   */
  addDisclosure(disclosure: Disclosure): this {
    this.disclosures.push(disclosure);
    return this;
  }

  /**
   * Set memo
   */
  setMemo(memo: Uint8Array): this {
    this.memo = memo;
    return this;
  }

  /**
   * Build the transaction
   */
  async build(): Promise<Transaction> {
    // Calculate total output value
    const totalOutput = this.outputs.reduce((sum, o) => sum + o.value, BigInt(0));
    const totalRequired = totalOutput + this.fee;

    // Select input notes
    this.inputs = selectNotesForSpending(this.availableNotes, totalRequired);
    const totalInput = this.inputs.reduce((sum, n) => sum + n.value, BigInt(0));

    // Calculate change
    const change = totalInput - totalRequired;
    if (change > BigInt(0)) {
      this.outputs.push({
        address: this.keys.address,
        value: change,
      });
    }

    // Generate nullifiers for inputs
    const nullifiers = this.inputs.map(note =>
      deriveNullifier(this.keys.spendingKey, note.commitment, note.position)
    );

    // Create output notes
    const outputNotes = createOutputNotes(this.outputs);
    const commitments = outputNotes.map(n => n.commitment);

    // Encrypt outputs for recipients
    const encryptedOutputs = outputNotes.map((note, i) => {
      // In production, would get recipient's viewing key
      return encryptNote(note, this.keys.viewingKey);
    });

    // Generate zk-SNARK proof
    const proofData = await this.generateTransactionProof(
      nullifiers,
      commitments
    );

    // Calculate disclosure flags
    const disclosureFlags = this.calculateDisclosureFlags();

    return {
      version: 1,
      nullifiers,
      commitments,
      encryptedOutputs,
      proof: proofData.proof,
      anchor: this.anchor,
      fee: this.fee,
      disclosureFlags,
      disclosures: this.disclosures,
      memo: this.memo,
    };
  }

  /**
   * Generate the transaction proof
   */
  private async generateTransactionProof(
    nullifiers: Hash[],
    commitments: Hash[]
  ): Promise<ProofData> {
    // Build witness for the circuit
    const witness = {
      // Public inputs
      merkleRoot: bytesToHex(this.anchor),
      nullifiers: nullifiers.map(n => bytesToHex(n)),
      commitments: commitments.map(c => bytesToHex(c)),
      fee: this.fee.toString(),

      // Private inputs
      spendingKey: bytesToHex(this.keys.spendingKey),
      inputValues: this.inputs.map(n => n.value.toString()),
      inputBlinders: this.inputs.map(n => bytesToHex(n.blinder)),
      outputValues: this.outputs.map(o => o.value.toString()),
      outputBlinders: [], // Would be set from output notes
    };

    return generateProof('transaction', witness);
  }

  /**
   * Calculate disclosure flags bitmask
   */
  private calculateDisclosureFlags(): number {
    let flags = 0;
    for (const d of this.disclosures) {
      switch (d.type) {
        case 'range':
          flags |= 0x01;
          break;
        case 'identity':
          flags |= 0x02;
          break;
        case 'temporal':
          flags |= 0x04;
          break;
        case 'sanctions':
          flags |= 0x08;
          break;
      }
    }
    return flags;
  }
}

/**
 * Create a simple transfer transaction
 */
export async function createTransfer(
  keys: WalletKeys,
  notes: Note[],
  anchor: Hash,
  recipient: Address,
  amount: bigint,
  fee: bigint
): Promise<Transaction> {
  const builder = new TransactionBuilder(keys, notes, anchor);

  return builder
    .addOutputs([{ address: recipient, value: amount }])
    .setFee(fee)
    .build();
}

/**
 * Serialize transaction for network transmission
 */
export function serializeTransaction(tx: Transaction): Uint8Array {
  // Simple serialization - in production would use proper encoding
  const parts: Uint8Array[] = [];

  // Version
  parts.push(new Uint8Array([tx.version]));

  // Nullifiers count and data
  parts.push(new Uint8Array([tx.nullifiers.length]));
  tx.nullifiers.forEach(n => parts.push(n));

  // Commitments count and data
  parts.push(new Uint8Array([tx.commitments.length]));
  tx.commitments.forEach(c => parts.push(c));

  // Proof length and data
  const proofLen = bigintToBytes(BigInt(tx.proof.length), 4);
  parts.push(proofLen);
  parts.push(tx.proof);

  // Anchor
  parts.push(tx.anchor);

  // Fee
  parts.push(bigintToBytes(tx.fee, 8));

  // Disclosure flags
  parts.push(new Uint8Array([tx.disclosureFlags]));

  // Combine all parts
  const totalLength = parts.reduce((sum, p) => sum + p.length, 0);
  const result = new Uint8Array(totalLength);
  let offset = 0;
  for (const part of parts) {
    result.set(part, offset);
    offset += part.length;
  }

  return result;
}

/**
 * Calculate transaction hash
 */
export function hashTransaction(tx: Transaction): Hash {
  const serialized = serializeTransaction(tx);
  // Would use proper hash in production
  const { sha256 } = require('@noble/hashes/sha256');
  return sha256(serialized);
}
