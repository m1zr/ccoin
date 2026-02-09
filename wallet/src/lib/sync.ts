// Wallet synchronization with the blockchain
import {
  WalletKeys,
  WalletState,
  Note,
  Hash,
  decryptNote,
  deriveNullifier,
  bytesToHex,
  hexToBytes,
} from './crypto';

/**
 * RPC client configuration
 */
export interface RpcConfig {
  endpoint: string;
  timeout?: number;
}

/**
 * Block header from RPC
 */
export interface BlockHeader {
  hash: string;
  height: number;
  timestamp: number;
  merkleRoot: string;
}

/**
 * Transaction from RPC
 */
export interface RpcTransaction {
  hash: string;
  blockHeight: number;
  nullifiers: string[];
  commitments: string[];
  encryptedOutputs: string[];
}

/**
 * Sync status
 */
export interface SyncStatus {
  currentHeight: number;
  targetHeight: number;
  percentage: number;
  syncing: boolean;
}

/**
 * Wallet sync client
 */
export class WalletSync {
  private config: RpcConfig;
  private state: WalletState;
  private syncStatus: SyncStatus;
  private syncAborted = false;

  constructor(config: RpcConfig, state: WalletState) {
    this.config = config;
    this.state = state;
    this.syncStatus = {
      currentHeight: state.syncedHeight,
      targetHeight: 0,
      percentage: 0,
      syncing: false,
    };
  }

  /**
   * Start synchronization
   */
  async sync(onProgress?: (status: SyncStatus) => void): Promise<void> {
    if (this.syncStatus.syncing) {
      throw new Error('Already syncing');
    }

    this.syncStatus.syncing = true;
    this.syncAborted = false;

    try {
      // Get current chain height
      const targetHeight = await this.getChainHeight();
      this.syncStatus.targetHeight = targetHeight;

      // Sync blocks in batches
      const BATCH_SIZE = 100;
      let currentHeight = this.state.syncedHeight;

      while (currentHeight < targetHeight && !this.syncAborted) {
        const endHeight = Math.min(currentHeight + BATCH_SIZE, targetHeight);
        
        // Fetch and process blocks
        const blocks = await this.fetchBlocks(currentHeight + 1, endHeight);
        
        for (const block of blocks) {
          await this.processBlock(block);
        }

        currentHeight = endHeight;
        this.state.syncedHeight = currentHeight;
        this.syncStatus.currentHeight = currentHeight;
        this.syncStatus.percentage = Math.floor(
          (currentHeight / targetHeight) * 100
        );

        if (onProgress) {
          onProgress(this.syncStatus);
        }
      }
    } finally {
      this.syncStatus.syncing = false;
    }
  }

  /**
   * Stop synchronization
   */
  abort(): void {
    this.syncAborted = true;
  }

  /**
   * Get current sync status
   */
  getStatus(): SyncStatus {
    return { ...this.syncStatus };
  }

  /**
   * Get chain height from RPC
   */
  private async getChainHeight(): Promise<number> {
    const response = await this.rpcCall('getBlockCount', []);
    return response.result;
  }

  /**
   * Fetch blocks from RPC
   */
  private async fetchBlocks(
    startHeight: number,
    endHeight: number
  ): Promise<RpcBlock[]> {
    const response = await this.rpcCall('getBlocks', [startHeight, endHeight]);
    return response.result;
  }

  /**
   * Process a single block
   */
  private async processBlock(block: RpcBlock): Promise<void> {
    for (const tx of block.transactions) {
      // Check for our nullifiers (spent notes)
      for (const nullifier of tx.nullifiers) {
        if (this.state.nullifiers.has(nullifier)) {
          // Mark corresponding note as spent
          for (const note of this.state.notes) {
            const expectedNullifier = deriveNullifier(
              this.state.keys.spendingKey,
              note.commitment,
              note.position
            );
            if (bytesToHex(expectedNullifier) === nullifier) {
              note.spent = true;
            }
          }
        }
      }

      // Try to decrypt outputs (our received notes)
      for (let i = 0; i < tx.encryptedOutputs.length; i++) {
        const ciphertext = hexToBytes(tx.encryptedOutputs[i]);
        const commitment = hexToBytes(tx.commitments[i]);

        const note = decryptNote(
          ciphertext,
          this.state.keys.viewingKey,
          commitment,
          0 // Position would come from tree
        );

        if (note) {
          note.blockHeight = block.height;
          note.position = this.calculatePosition(block.height, tx.hash, i);
          this.state.notes.push(note);

          // Track nullifier
          const nullifier = deriveNullifier(
            this.state.keys.spendingKey,
            note.commitment,
            note.position
          );
          this.state.nullifiers.add(bytesToHex(nullifier));
        }
      }
    }
  }

  /**
   * Calculate note position in commitment tree
   */
  private calculatePosition(
    blockHeight: number,
    txHash: string,
    outputIndex: number
  ): number {
    // Simplified - in production would query tree
    return blockHeight * 1000 + outputIndex;
  }

  /**
   * Make RPC call
   */
  private async rpcCall(
    method: string,
    params: unknown[]
  ): Promise<{ result: any }> {
    const response = await fetch(this.config.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: Date.now(),
        method,
        params,
      }),
    });

    if (!response.ok) {
      throw new Error(`RPC error: ${response.status}`);
    }

    const data = await response.json();
    if (data.error) {
      throw new Error(`RPC error: ${data.error.message}`);
    }

    return data;
  }
}

/**
 * RPC block response
 */
interface RpcBlock {
  hash: string;
  height: number;
  timestamp: number;
  transactions: RpcTransaction[];
}

/**
 * Merkle tree client for commitment tree operations
 */
export class MerkleTreeClient {
  constructor(private rpcConfig: RpcConfig) {}

  /**
   * Get current merkle root
   */
  async getRoot(): Promise<Hash> {
    const response = await this.rpcCall('getMerkleRoot', []);
    return hexToBytes(response.result);
  }

  /**
   * Get merkle path for a position
   */
  async getPath(position: number): Promise<MerklePath> {
    const response = await this.rpcCall('getMerklePath', [position]);
    return {
      siblings: response.result.siblings.map((s: string) => hexToBytes(s)),
      indices: response.result.indices,
    };
  }

  private async rpcCall(
    method: string,
    params: unknown[]
  ): Promise<{ result: any }> {
    const response = await fetch(this.rpcConfig.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: Date.now(),
        method,
        params,
      }),
    });

    return response.json();
  }
}

/**
 * Merkle path for proof generation
 */
export interface MerklePath {
  siblings: Hash[];
  indices: number[];
}

/**
 * Persist wallet state to storage
 */
export function saveWalletState(state: WalletState, key: string): void {
  const serialized = JSON.stringify({
    keys: {
      spendingKey: bytesToHex(state.keys.spendingKey),
      viewingKey: bytesToHex(state.keys.viewingKey),
      address: bytesToHex(state.keys.address),
    },
    notes: state.notes.map(n => ({
      ...n,
      address: bytesToHex(n.address),
      blinder: bytesToHex(n.blinder),
      commitment: bytesToHex(n.commitment),
      memo: n.memo ? bytesToHex(n.memo) : undefined,
    })),
    nullifiers: Array.from(state.nullifiers),
    syncedHeight: state.syncedHeight,
  });

  localStorage.setItem(key, serialized);
}

/**
 * Load wallet state from storage
 */
export function loadWalletState(key: string): WalletState | null {
  const serialized = localStorage.getItem(key);
  if (!serialized) return null;

  const data = JSON.parse(serialized);
  return {
    keys: {
      spendingKey: hexToBytes(data.keys.spendingKey),
      viewingKey: hexToBytes(data.keys.viewingKey),
      address: hexToBytes(data.keys.address),
    },
    notes: data.notes.map((n: any) => ({
      ...n,
      value: BigInt(n.value),
      address: hexToBytes(n.address),
      blinder: hexToBytes(n.blinder),
      commitment: hexToBytes(n.commitment),
      memo: n.memo ? hexToBytes(n.memo) : undefined,
    })),
    pendingNotes: [],
    nullifiers: new Set(data.nullifiers),
    syncedHeight: data.syncedHeight,
  };
}
