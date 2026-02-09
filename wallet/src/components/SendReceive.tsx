'use client';

import React, { useState } from 'react';
import { WalletState, Address, hexToBytes } from '../lib/crypto';
import { TransactionBuilder, Transaction } from '../lib/transaction';

interface SendFormProps {
  state: WalletState;
  anchor: Uint8Array;
  onSubmit: (tx: Transaction) => Promise<void>;
  onCancel: () => void;
}

export function SendForm({ state, anchor, onSubmit, onCancel }: SendFormProps) {
  const [recipient, setRecipient] = useState('');
  const [amount, setAmount] = useState('');
  const [memo, setMemo] = useState('');
  const [fee, setFee] = useState('0.0001');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Disclosure options
  const [showDisclosures, setShowDisclosures] = useState(false);
  const [disclosures, setDisclosures] = useState({
    range: false,
    rangeMin: '',
    rangeMax: '',
    identity: false,
    temporal: false,
    temporalMinDays: '',
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);

    try {
      // Parse inputs
      const recipientAddr = hexToBytes(recipient) as Address;
      const amountValue = parseAmount(amount);
      const feeValue = parseAmount(fee);

      // Build transaction
      const builder = new TransactionBuilder(state.keys, state.notes, anchor);
      builder
        .addOutputs([{ address: recipientAddr, value: amountValue }])
        .setFee(feeValue);

      if (memo) {
        builder.setMemo(new TextEncoder().encode(memo));
      }

      // Add disclosures if selected
      if (disclosures.range) {
        // Would generate range proof here
      }

      const tx = await builder.build();
      await onSubmit(tx);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Transaction failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="send-form">
      <h2>Send CCOIN</h2>

      <form onSubmit={handleSubmit}>
        <div className="form-group">
          <label>Recipient Address</label>
          <input
            type="text"
            value={recipient}
            onChange={e => setRecipient(e.target.value)}
            placeholder="Enter shielded address"
            required
          />
        </div>

        <div className="form-group">
          <label>Amount</label>
          <div className="amount-input">
            <input
              type="text"
              value={amount}
              onChange={e => setAmount(e.target.value)}
              placeholder="0.00"
              required
            />
            <span className="symbol">CCOIN</span>
          </div>
        </div>

        <div className="form-group">
          <label>Memo (optional)</label>
          <input
            type="text"
            value={memo}
            onChange={e => setMemo(e.target.value)}
            placeholder="Add a note"
            maxLength={512}
          />
        </div>

        <div className="form-group">
          <label>Network Fee</label>
          <select value={fee} onChange={e => setFee(e.target.value)}>
            <option value="0.0001">Standard (0.0001)</option>
            <option value="0.0005">Priority (0.0005)</option>
            <option value="0.001">Express (0.001)</option>
          </select>
        </div>

        <div className="disclosure-section">
          <button
            type="button"
            className="toggle-disclosures"
            onClick={() => setShowDisclosures(!showDisclosures)}
          >
            {showDisclosures ? '▼' : '▶'} Programmable Disclosures
          </button>

          {showDisclosures && (
            <div className="disclosure-options">
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={disclosures.range}
                  onChange={e =>
                    setDisclosures({ ...disclosures, range: e.target.checked })
                  }
                />
                Range Proof (prove amount is within range)
              </label>

              {disclosures.range && (
                <div className="range-inputs">
                  <input
                    type="text"
                    placeholder="Min"
                    value={disclosures.rangeMin}
                    onChange={e =>
                      setDisclosures({ ...disclosures, rangeMin: e.target.value })
                    }
                  />
                  <span>to</span>
                  <input
                    type="text"
                    placeholder="Max"
                    value={disclosures.rangeMax}
                    onChange={e =>
                      setDisclosures({ ...disclosures, rangeMax: e.target.value })
                    }
                  />
                </div>
              )}

              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={disclosures.identity}
                  onChange={e =>
                    setDisclosures({ ...disclosures, identity: e.target.checked })
                  }
                />
                Identity Attestation
              </label>

              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={disclosures.temporal}
                  onChange={e =>
                    setDisclosures({ ...disclosures, temporal: e.target.checked })
                  }
                />
                Temporal Proof (funds held for duration)
              </label>

              {disclosures.temporal && (
                <div className="temporal-input">
                  <input
                    type="text"
                    placeholder="Minimum days"
                    value={disclosures.temporalMinDays}
                    onChange={e =>
                      setDisclosures({
                        ...disclosures,
                        temporalMinDays: e.target.value,
                      })
                    }
                  />
                  <span>days</span>
                </div>
              )}
            </div>
          )}
        </div>

        {error && <div className="error-message">{error}</div>}

        <div className="form-actions">
          <button type="button" className="cancel-btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className="submit-btn" disabled={loading}>
            {loading ? 'Building Proof...' : 'Send'}
          </button>
        </div>
      </form>
    </div>
  );
}

function parseAmount(value: string): bigint {
  const [whole, frac = ''] = value.split('.');
  const wholeNum = BigInt(whole || '0');
  const fracPadded = frac.padEnd(8, '0').slice(0, 8);
  const fracNum = BigInt(fracPadded);
  return wholeNum * BigInt(1e8) + fracNum;
}

// Receive address display component
interface ReceiveProps {
  address: Uint8Array;
  onClose: () => void;
}

export function ReceiveAddress({ address, onClose }: ReceiveProps) {
  const addressHex = Array.from(address)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');

  const copyAddress = () => {
    navigator.clipboard.writeText(addressHex);
  };

  return (
    <div className="receive-modal">
      <h2>Receive CCOIN</h2>
      <p>Share your shielded address to receive funds privately.</p>

      <div className="qr-placeholder">
        {/* QR code would go here */}
        <div className="qr-code">QR</div>
      </div>

      <div className="address-display">
        <code>{addressHex}</code>
      </div>

      <div className="modal-actions">
        <button onClick={copyAddress}>Copy Address</button>
        <button onClick={onClose}>Close</button>
      </div>
    </div>
  );
}
