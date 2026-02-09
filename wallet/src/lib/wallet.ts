/**
 * CCoin Wallet Core Library
 * Handles key generation, address derivation, and transaction signing
 */

import { sha256 } from '@noble/hashes/sha256';
import { randomBytes } from '@noble/hashes/utils';

// Constants
export const SPENDING_KEY_SIZE = 32;
export const VIEWING_KEY_SIZE = 32;
export const ADDRESS_SIZE = 20;

/**
 * Spending Key - used to spend funds (must be kept secret)
 */
export interface SpendingKey {
  bytes: Uint8Array;
}

/**
 * Viewing Key - derived from spending key, allows viewing transactions
 */
export interface ViewingKey {
  bytes: Uint8Array;
}

/**
 * Full Viewing Key - allows viewing all transaction details
 */
export interface FullViewingKey {
  spendingViewKey: ViewingKey;
  receiveViewKey: ViewingKey;
}

/**
 * Payment Address - public address for receiving funds
 */
export interface Address {
  bytes: Uint8Array;
  encoded: string;
}

/**
 * Wallet contains all key material for a CCoin account
 */
export interface Wallet {
  spendingKey: SpendingKey;
  viewingKey: FullViewingKey;
  address: Address;
  createdAt: number;
}

/**
 * Note represents a spendable output
 */
export interface Note {
  value: bigint;
  address: Address;
  blinder: Uint8Array;
  commitment: Uint8Array;
  nullifier: Uint8Array;
}

/**
 * Transaction represents a shielded transaction
 */
export interface Transaction {
  nullifiers: Uint8Array[];
  commitments: Uint8Array[];
  proof: Uint8Array;
  fee: bigint;
  memo?: Uint8Array;
}

/**
 * Generate a new random spending key
 */
export function generateSpendingKey(): SpendingKey {
  return {
    bytes: randomBytes(SPENDING_KEY_SIZE)
  };
}

/**
 * Derive viewing key from spending key
 */
export function deriveViewingKey(spendingKey: SpendingKey): FullViewingKey {
  // Derive keys using domain separation
  const spendViewKey = sha256(new Uint8Array([...spendingKey.bytes, 0x01]));
  const receiveViewKey = sha256(new Uint8Array([...spendingKey.bytes, 0x02]));
  
  return {
    spendingViewKey: { bytes: spendViewKey },
    receiveViewKey: { bytes: receiveViewKey }
  };
}

/**
 * Derive payment address from viewing key
 */
export function deriveAddress(viewingKey: FullViewingKey): Address {
  // Hash viewing keys to get address
  const combined = new Uint8Array([
    ...viewingKey.spendingViewKey.bytes,
    ...viewingKey.receiveViewKey.bytes
  ]);
  const hash = sha256(combined);
  const addressBytes = hash.slice(0, ADDRESS_SIZE);
  
  return {
    bytes: addressBytes,
    encoded: encodeAddress(addressBytes)
  };
}

/**
 * Create a new wallet
 */
export function createWallet(): Wallet {
  const spendingKey = generateSpendingKey();
  const viewingKey = deriveViewingKey(spendingKey);
  const address = deriveAddress(viewingKey);
  
  return {
    spendingKey,
    viewingKey,
    address,
    createdAt: Date.now()
  };
}

/**
 * Generate a Pedersen commitment for a note
 * commitment = H(value || address || blinder)
 */
export function createCommitment(value: bigint, address: Address, blinder: Uint8Array): Uint8Array {
  const valueBytes = bigintToBytes(value, 8);
  const data = new Uint8Array([...valueBytes, ...address.bytes, ...blinder]);
  return sha256(data);
}

/**
 * Derive nullifier for spending a note
 * nullifier = H(spending_key || commitment || position)
 */
export function deriveNullifier(
  spendingKey: SpendingKey,
  commitment: Uint8Array,
  position: bigint
): Uint8Array {
  const posBytes = bigintToBytes(position, 8);
  const data = new Uint8Array([...spendingKey.bytes, ...commitment, ...posBytes]);
  return sha256(data);
}

/**
 * Create a new note
 */
export function createNote(value: bigint, address: Address): Note {
  const blinder = randomBytes(32);
  const commitment = createCommitment(value, address, blinder);
  
  return {
    value,
    address,
    blinder,
    commitment,
    nullifier: new Uint8Array(32) // Set when spending
  };
}

/**
 * Encode address to bech32-like string
 */
export function encodeAddress(bytes: Uint8Array): string {
  const prefix = 'ccoin';
  const hex = bytesToHex(bytes);
  return `${prefix}1${hex}`;
}

/**
 * Decode address from string
 */
export function decodeAddress(encoded: string): Address {
  if (!encoded.startsWith('ccoin1')) {
    throw new Error('Invalid address prefix');
  }
  const hex = encoded.slice(6);
  const bytes = hexToBytes(hex);
  
  return { bytes, encoded };
}

// Utility functions
function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
  }
  return bytes;
}

function bigintToBytes(value: bigint, length: number): Uint8Array {
  const bytes = new Uint8Array(length);
  for (let i = length - 1; i >= 0; i--) {
    bytes[i] = Number(value & 0xffn);
    value >>= 8n;
  }
  return bytes;
}

/**
 * Wallet state store interface
 */
export interface WalletStore {
  wallet: Wallet | null;
  balance: bigint;
  notes: Note[];
  pendingTx: Transaction[];
  
  createWallet: () => void;
  loadWallet: (spendingKey: SpendingKey) => void;
  getBalance: () => bigint;
  getNotes: () => Note[];
}
