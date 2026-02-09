# Project CCoin - README

> **The Decentralized AI Economy** - A private, scalable cryptocurrency that transforms mining energy into collectively-owned artificial intelligence.

## Overview

CCoin is a next-generation cryptocurrency implementing:

- **BlockDAG Consensus** - Parallel block processing with reputation-weighted ordering
- **Proof-of-Useful-Work (PoUW)** - Mining that trains AI models instead of wasting energy
- **zk-SNARK Privacy** - Fully shielded transactions with programmable selective disclosure
- **AI Commons** - Collectively-owned AI models governed by token holders
- **AI Oracle Layer** - Cross-chain AI inference service

## Project Structure

```
ccoin/
├── core/                   # Go - Core protocol (Layer 1)
│   ├── cmd/
│   │   ├── ccoind/         # Main daemon
│   │   └── ccoin-cli/      # CLI interface
│   ├── internal/
│   │   ├── dag/            # BlockDAG implementation
│   │   ├── consensus/      # Reputation-weighted consensus
│   │   ├── storage/        # PostgreSQL storage layer
│   │   └── reputation/     # Miner reputation system
│   ├── pkg/
│   │   ├── types/          # Core type definitions
│   │   └── common/         # Shared utilities
│   └── migrations/         # Database migrations
│
├── wallet/                 # TypeScript/React - Wallet (Layer 2)
│   └── src/lib/            # Wallet core & zk-SNARK integration
│
├── inference/              # Python - Inference Layer (Layer 3)
│   └── src/                # FastAPI inference server
│
└── docker/                 # Docker configurations
```

## Quick Start

### Prerequisites

- Go 1.21+
- Node.js 20+
- Python 3.10+
- PostgreSQL 16+
- Docker & Docker Compose

### Development Setup

1. **Start the database and services:**
   ```bash
   cd docker
   docker-compose up -d postgres redis
   ```

2. **Initialize the database:**
   ```bash
   psql -h localhost -U ccoin -d ccoin -f core/migrations/001_initial.sql
   ```

3. **Build and run the node:**
   ```bash
   cd core
   go mod tidy
   go build -o ccoind ./cmd/ccoind
   ./ccoind --db-host=localhost --db-user=ccoin --db-name=ccoin
   ```

4. **Run the wallet (development):**
   ```bash
   cd wallet
   npm install
   npm run dev
   ```

5. **Run the inference server:**
   ```bash
   cd inference
   pip install -e .
   python -m uvicorn src.main:app --reload
   ```

## Architecture

### BlockDAG
Blocks reference multiple parents, allowing parallel block creation. Ordering uses reputation-weighted GHOST:

```
S(B) = Work(B) × Rep(m) + Σ S(Children)
```

### Proof-of-Useful-Work
Mining computes gradients for AI model training. Validity requires:
1. `H(Header || nonce || Hash(R)) < Difficulty`
2. `Loss(W_t + α·∇L) < Loss(W_t)` (Improvement Gate)

### Privacy Layer
Transactions use zk-SNARKs (Groth16) with optional programmable disclosures:
- Range Disclosure: Prove amount is within bounds
- Identity Disclosure: Prove ownership of credential
- Temporal Disclosure: Prove funds held for minimum duration

### Reputation System
Miner reputation uses EWMA:
```
Rep_t = λ × Rep_{t-1} + (1 - λ) × Q̄_epoch
```

Higher reputation = higher block weight, higher rewards.

## Tokenomics

| Parameter | Value |
|---|---|
| Max Supply | 210,000,000 CCoin |
| Initial Block Reward | 50 CCoin |
| Halving Interval | 2,100,000 blocks |
| Tail Emission | 0.001 CCoin |

## License

MIT License - See LICENSE file for details.

## Documentation

- [Whitepaper](./Project_CCoin_Whitepaper_v2.md)
- [Implementation Plan](./docs/implementation_plan.md)
