'use client';

import React, { useState, useEffect } from 'react';
import { WalletState, Note, calculateBalance, bytesToHex } from '../lib/crypto';
import { SyncStatus } from '../lib/sync';

interface WalletDashboardProps {
  state: WalletState;
  syncStatus: SyncStatus;
  onSend: () => void;
  onReceive: () => void;
  onSync: () => void;
}

export function WalletDashboard({
  state,
  syncStatus,
  onSend,
  onReceive,
  onSync,
}: WalletDashboardProps) {
  const balance = calculateBalance(state.notes);
  const formattedBalance = formatBalance(balance);
  const pendingBalance = calculateBalance(state.pendingNotes);

  return (
    <div className="wallet-dashboard">
      <header className="wallet-header">
        <h1>CCoin Wallet</h1>
        <div className="sync-status">
          {syncStatus.syncing ? (
            <span className="syncing">
              Syncing... {syncStatus.percentage}%
            </span>
          ) : (
            <span className="synced">
              Synced to block {syncStatus.currentHeight}
            </span>
          )}
        </div>
      </header>

      <section className="balance-section">
        <div className="balance-card">
          <h2>Shielded Balance</h2>
          <div className="balance-amount">
            <span className="amount">{formattedBalance}</span>
            <span className="symbol">CCOIN</span>
          </div>
          {pendingBalance > 0 && (
            <div className="pending-balance">
              +{formatBalance(pendingBalance)} pending
            </div>
          )}
        </div>
      </section>

      <section className="actions-section">
        <button className="action-btn send" onClick={onSend}>
          <SendIcon />
          Send
        </button>
        <button className="action-btn receive" onClick={onReceive}>
          <ReceiveIcon />
          Receive
        </button>
        <button className="action-btn sync" onClick={onSync}>
          <SyncIcon />
          Sync
        </button>
      </section>

      <section className="notes-section">
        <h3>Shielded Notes ({state.notes.filter(n => !n.spent).length})</h3>
        <div className="notes-list">
          {state.notes
            .filter(n => !n.spent)
            .map((note, index) => (
              <NoteCard key={index} note={note} />
            ))}
        </div>
      </section>

      <section className="address-section">
        <h3>Shielded Address</h3>
        <div className="address-display">
          <code>{bytesToHex(state.keys.address)}</code>
          <button
            className="copy-btn"
            onClick={() => copyToClipboard(bytesToHex(state.keys.address))}
          >
            Copy
          </button>
        </div>
      </section>
    </div>
  );
}

interface NoteCardProps {
  note: Note;
}

function NoteCard({ note }: NoteCardProps) {
  return (
    <div className={`note-card ${note.spent ? 'spent' : ''}`}>
      <div className="note-value">{formatBalance(note.value)} CCOIN</div>
      <div className="note-details">
        <span>Block #{note.blockHeight}</span>
        <span>Position: {note.position}</span>
      </div>
    </div>
  );
}

// Helper functions

function formatBalance(amount: bigint): string {
  const whole = amount / BigInt(1e8);
  const frac = amount % BigInt(1e8);
  if (frac === BigInt(0)) {
    return whole.toString();
  }
  const fracStr = frac.toString().padStart(8, '0').replace(/0+$/, '');
  return `${whole}.${fracStr}`;
}

function copyToClipboard(text: string): void {
  navigator.clipboard.writeText(text);
}

// Icons

function SendIcon() {
  return (
    <svg viewBox="0 0 24 24" width="24" height="24">
      <path
        fill="currentColor"
        d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"
      />
    </svg>
  );
}

function ReceiveIcon() {
  return (
    <svg viewBox="0 0 24 24" width="24" height="24">
      <path
        fill="currentColor"
        d="M20 5.41L18.59 4 7 15.59V9H5v10h10v-2H8.41z"
      />
    </svg>
  );
}

function SyncIcon() {
  return (
    <svg viewBox="0 0 24 24" width="24" height="24">
      <path
        fill="currentColor"
        d="M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46C19.54 15.03 20 13.57 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74C4.46 8.97 4 10.43 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z"
      />
    </svg>
  );
}
