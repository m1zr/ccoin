// Core cryptographic operations for wallet
import { sha256 } from '@noble/hashes/sha256';
import { randomBytes } from '@noble/hashes/utils';

// Constants
export const HASH_SIZE = 32;
export const ADDRESS_SIZE = 20;
export const SPENDING_KEY_SIZE = 32;
export const VIEWING_KEY_SIZE = 32;

// Types
export type Hash = Uint8Array;
export type Address = Uint8Array;
export type SpendingKey = Uint8Array;
export type ViewingKey = Uint8Array;

/**
 * Full wallet key set
 */
export interface WalletKeys {
  spendingKey: SpendingKey;
  viewingKey: ViewingKey;
  address: Address;
}

/**
 * Shielded note (UTXO)
 */
export interface Note {
  value: bigint;
  address: Address;
  blinder: Uint8Array;
  commitment: Hash;
  position: number;
  blockHeight: number;
  spent: boolean;
  memo?: Uint8Array;
}

/**
 * Wallet state
 */
export interface WalletState {
  keys: WalletKeys;
  notes: Note[];
  pendingNotes: Note[];
  nullifiers: Set<string>;
  syncedHeight: number;
}

/**
 * Generate new wallet keys
 */
export function generateKeys(): WalletKeys {
  const spendingKey = randomBytes(SPENDING_KEY_SIZE);
  const viewingKey = deriveViewingKey(spendingKey);
  const address = deriveAddress(viewingKey);

  return { spendingKey, viewingKey, address };
}

/**
 * Derive viewing key from spending key
 */
export function deriveViewingKey(spendingKey: SpendingKey): ViewingKey {
  return sha256(new Uint8Array([...spendingKey, 0x01]));
}

/**
 * Derive address from viewing key
 */
export function deriveAddress(viewingKey: ViewingKey): Address {
  const hash = sha256(new Uint8Array([...viewingKey, 0x02]));
  return hash.slice(0, ADDRESS_SIZE);
}

/**
 * Create a Pedersen commitment
 * C = value * G + blinder * H
 */
export function createCommitment(value: bigint, blinder: Uint8Array): Hash {
  // Simplified - in production would use actual curve operations
  const valueBytes = bigintToBytes(value, 8);
  const data = new Uint8Array([...valueBytes, ...blinder]);
  return sha256(data);
}

/**
 * Derive nullifier for a note
 * nf = H(spending_key || commitment || position)
 */
export function deriveNullifier(
  spendingKey: SpendingKey,
  commitment: Hash,
  position: number
): Hash {
  const posBytes = numberToBytes(position, 4);
  const data = new Uint8Array([...spendingKey, ...commitment, ...posBytes]);
  return sha256(data);
}

/**
 * Calculate wallet balance from unspent notes
 */
export function calculateBalance(notes: Note[]): bigint {
  return notes
    .filter(n => !n.spent)
    .reduce((sum, n) => sum + n.value, BigInt(0));
}

/**
 * Select notes for spending
 * Uses simple greedy selection
 */
export function selectNotesForSpending(
  notes: Note[],
  targetAmount: bigint
): Note[] {
  const available = notes.filter(n => !n.spent);
  
  // Sort by value descending
  available.sort((a, b) => {
    if (a.value > b.value) return -1;
    if (a.value < b.value) return 1;
    return 0;
  });

  const selected: Note[] = [];
  let total = BigInt(0);

  for (const note of available) {
    if (total >= targetAmount) break;
    selected.push(note);
    total += note.value;
  }

  if (total < targetAmount) {
    throw new Error('Insufficient funds');
  }

  return selected;
}

/**
 * Create output notes for a transaction
 */
export function createOutputNotes(
  outputs: Array<{ address: Address; value: bigint; memo?: Uint8Array }>
): Note[] {
  return outputs.map((out, index) => {
    const blinder = randomBytes(32);
    const commitment = createCommitment(out.value, blinder);

    return {
      value: out.value,
      address: out.address,
      blinder,
      commitment,
      position: -1, // Will be set when added to tree
      blockHeight: 0,
      spent: false,
      memo: out.memo,
    };
  });
}

/**
 * Encrypt note for recipient
 */
export function encryptNote(note: Note, recipientViewingKey: ViewingKey): Uint8Array {
  // Simplified encryption - in production would use proper ECIES
  const plaintext = new Uint8Array([
    ...bigintToBytes(note.value, 8),
    ...note.blinder,
    ...(note.memo || []),
  ]);

  // XOR with key-derived stream (simplified)
  const keyStream = sha256(recipientViewingKey);
  const ciphertext = new Uint8Array(plaintext.length);
  for (let i = 0; i < plaintext.length; i++) {
    ciphertext[i] = plaintext[i] ^ keyStream[i % keyStream.length];
  }

  return ciphertext;
}

/**
 * Decrypt note with viewing key
 */
export function decryptNote(
  ciphertext: Uint8Array,
  viewingKey: ViewingKey,
  commitment: Hash,
  position: number
): Note | null {
  try {
    // XOR decrypt
    const keyStream = sha256(viewingKey);
    const plaintext = new Uint8Array(ciphertext.length);
    for (let i = 0; i < ciphertext.length; i++) {
      plaintext[i] = ciphertext[i] ^ keyStream[i % keyStream.length];
    }

    const value = bytesToBigint(plaintext.slice(0, 8));
    const blinder = plaintext.slice(8, 40);
    const memo = plaintext.length > 40 ? plaintext.slice(40) : undefined;

    // Verify commitment
    const computedCommitment = createCommitment(value, blinder);
    if (!arraysEqual(computedCommitment, commitment)) {
      return null; // Not for us
    }

    return {
      value,
      address: deriveAddress(viewingKey),
      blinder,
      commitment,
      position,
      blockHeight: 0,
      spent: false,
      memo,
    };
  } catch {
    return null;
  }
}

// Utility functions

export function bigintToBytes(value: bigint, length: number): Uint8Array {
  const bytes = new Uint8Array(length);
  for (let i = length - 1; i >= 0; i--) {
    bytes[i] = Number(value & BigInt(0xff));
    value >>= BigInt(8);
  }
  return bytes;
}

export function bytesToBigint(bytes: Uint8Array): bigint {
  let value = BigInt(0);
  for (const byte of bytes) {
    value = (value << BigInt(8)) | BigInt(byte);
  }
  return value;
}

export function numberToBytes(value: number, length: number): Uint8Array {
  const bytes = new Uint8Array(length);
  for (let i = length - 1; i >= 0; i--) {
    bytes[i] = value & 0xff;
    value >>= 8;
  }
  return bytes;
}

export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

export function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16);
  }
  return bytes;
}

export function arraysEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}
