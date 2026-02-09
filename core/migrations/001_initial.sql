-- CCoin Database Schema v1.0
-- PostgreSQL schema for the CCoin blockchain

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-----------------------------------
-- BLOCKS TABLE (BlockDAG)
-----------------------------------
CREATE TABLE IF NOT EXISTS blocks (
    -- Primary key is the block hash
    hash BYTEA PRIMARY KEY CHECK (length(hash) = 32),
    
    -- Block version
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Array of parent block hashes (DAG structure)
    parents BYTEA[] NOT NULL DEFAULT '{}',
    
    -- Merkle root of transactions
    tx_root BYTEA NOT NULL CHECK (length(tx_root) = 32),
    
    -- State trie root after this block
    state_root BYTEA NOT NULL CHECK (length(state_root) = 32),
    
    -- PoUW computation result hash
    pouw_result BYTEA CHECK (length(pouw_result) = 32),
    
    -- PoUW verification proof data
    pouw_proof BYTEA,
    
    -- AI task this block contributes to
    task_id BYTEA CHECK (length(task_id) = 32),
    
    -- Quality score of the PoUW computation (0 < Q <= 1)
    quality_score DECIMAL(10, 9) CHECK (quality_score > 0 AND quality_score <= 1),
    
    -- Miner's address
    miner_address BYTEA NOT NULL CHECK (length(miner_address) = 20),
    
    -- Miner's reputation at time of mining
    reputation_score DECIMAL(10, 6) NOT NULL CHECK (reputation_score >= 0.1 AND reputation_score <= 3.0),
    
    -- Difficulty target
    difficulty BYTEA NOT NULL,
    
    -- Nonce that satisfies difficulty
    nonce BIGINT NOT NULL,
    
    -- Block creation timestamp (Unix seconds)
    timestamp BIGINT NOT NULL,
    
    -- Logical height in the DAG
    height BIGINT NOT NULL CHECK (height >= 0),
    
    -- Cumulative reputation-weighted score: S(B) = Work(B) * Rep(m) + sum(S(children))
    cumulative_score DECIMAL(78, 0) NOT NULL DEFAULT 0,
    
    -- Whether this block is on the main chain
    is_main_chain BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Extra data (max 32 bytes)
    extra_data BYTEA CHECK (length(extra_data) <= 32),
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    -- Indexes for efficient DAG traversal
    CONSTRAINT valid_parents CHECK (array_length(parents, 1) >= 0 OR parents = '{}')
);

-- Indexes for blocks
CREATE INDEX IF NOT EXISTS idx_blocks_height ON blocks(height);
CREATE INDEX IF NOT EXISTS idx_blocks_main_chain ON blocks(is_main_chain) WHERE is_main_chain = TRUE;
CREATE INDEX IF NOT EXISTS idx_blocks_miner ON blocks(miner_address);
CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks(timestamp);
CREATE INDEX IF NOT EXISTS idx_blocks_task ON blocks(task_id) WHERE task_id IS NOT NULL;

-----------------------------------
-- TRANSACTIONS TABLE (Shielded)
-----------------------------------
CREATE TABLE IF NOT EXISTS transactions (
    -- Transaction hash
    tx_hash BYTEA PRIMARY KEY CHECK (length(tx_hash) = 32),
    
    -- Block containing this transaction (NULL if pending)
    block_hash BYTEA REFERENCES blocks(hash) ON DELETE SET NULL,
    
    -- Transaction version
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Nullifiers (prevent double-spending)
    nullifiers BYTEA[] NOT NULL,
    
    -- Output commitments
    commitments BYTEA[] NOT NULL,
    
    -- zk-SNARK proof type (0 = Groth16, 1 = PLONK)
    proof_type SMALLINT NOT NULL DEFAULT 0,
    
    -- Serialized zk-SNARK proof
    proof BYTEA NOT NULL,
    
    -- Merkle tree anchor (commitment tree root at tx creation)
    anchor BYTEA NOT NULL CHECK (length(anchor) = 32),
    
    -- Disclosure flags bitmap
    disclosure_flags INTEGER NOT NULL DEFAULT 0,
    
    -- Serialized disclosure proofs
    disclosures BYTEA,
    
    -- Transaction fee (in base units)
    fee BIGINT NOT NULL CHECK (fee >= 0),
    
    -- Encrypted memo
    memo BYTEA,
    
    -- Position in block
    tx_index INTEGER,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for transactions
CREATE INDEX IF NOT EXISTS idx_transactions_block ON transactions(block_hash);
CREATE INDEX IF NOT EXISTS idx_transactions_pending ON transactions(block_hash) WHERE block_hash IS NULL;
CREATE INDEX IF NOT EXISTS idx_transactions_created ON transactions(created_at);

-----------------------------------
-- NULLIFIERS TABLE (Double-spend prevention)
-----------------------------------
CREATE TABLE IF NOT EXISTS nullifiers (
    -- Nullifier value
    nullifier BYTEA PRIMARY KEY CHECK (length(nullifier) = 32),
    
    -- Transaction that spent this nullifier
    tx_hash BYTEA NOT NULL REFERENCES transactions(tx_hash),
    
    -- Block height when spent
    block_height BIGINT NOT NULL
);

-- Index for nullifier queries
CREATE INDEX IF NOT EXISTS idx_nullifiers_height ON nullifiers(block_height);

-----------------------------------
-- COMMITMENTS TABLE (UTXO-style outputs)
-----------------------------------
CREATE TABLE IF NOT EXISTS commitments (
    -- Commitment value
    commitment BYTEA PRIMARY KEY CHECK (length(commitment) = 32),
    
    -- Position in the commitment Merkle tree
    tree_index BIGINT NOT NULL UNIQUE,
    
    -- Block height when created
    block_height BIGINT NOT NULL,
    
    -- Transaction that created this commitment
    tx_hash BYTEA NOT NULL REFERENCES transactions(tx_hash),
    
    -- Encrypted note data
    encrypted_note BYTEA
);

-- Index for commitment queries
CREATE INDEX IF NOT EXISTS idx_commitments_height ON commitments(block_height);
CREATE INDEX IF NOT EXISTS idx_commitments_tree_index ON commitments(tree_index);

-----------------------------------
-- MINERS TABLE (Reputation tracking)
-----------------------------------
CREATE TABLE IF NOT EXISTS miners (
    -- Miner's address
    address BYTEA PRIMARY KEY CHECK (length(address) = 20),
    
    -- Current reputation score [0.1, 3.0]
    reputation_score DECIMAL(10, 6) NOT NULL DEFAULT 1.0 
        CHECK (reputation_score >= 0.1 AND reputation_score <= 3.0),
    
    -- Total blocks mined
    total_blocks BIGINT NOT NULL DEFAULT 0,
    
    -- Sum of quality scores across all blocks
    total_quality_score DECIMAL(30, 10) NOT NULL DEFAULT 0,
    
    -- Blocks mined in current epoch
    epoch_blocks BIGINT NOT NULL DEFAULT 0,
    
    -- Quality score sum in current epoch
    epoch_quality_sum DECIMAL(20, 10) NOT NULL DEFAULT 0,
    
    -- Last active epoch number
    last_active_epoch BIGINT,
    
    -- Total rewards earned (in base units)
    total_rewards DECIMAL(30, 0) NOT NULL DEFAULT 0,
    
    -- Amount staked
    staked_amount DECIMAL(30, 0) NOT NULL DEFAULT 0,
    
    -- Ban status
    is_banned BOOLEAN NOT NULL DEFAULT FALSE,
    ban_expires_at BIGINT,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for active miners
CREATE INDEX IF NOT EXISTS idx_miners_reputation ON miners(reputation_score DESC);
CREATE INDEX IF NOT EXISTS idx_miners_active ON miners(last_active_epoch DESC);

-----------------------------------
-- MODELS TABLE (AI Commons Registry)
-----------------------------------
CREATE TABLE IF NOT EXISTS models (
    -- Unique model identifier
    model_id BYTEA PRIMARY KEY CHECK (length(model_id) = 32),
    
    -- Model architecture (e.g., "transformer-7B", "resnet-152")
    architecture VARCHAR(100) NOT NULL,
    
    -- Task type: CLASSIFICATION, GENERATION, REGRESSION, EMBEDDING, FOLDING, SIMULATION
    task_type VARCHAR(50) NOT NULL,
    
    -- Domain (e.g., "nlp", "medical-imaging", "climate")
    domain VARCHAR(100),
    
    -- IPFS CID of current model weights
    current_weights_cid VARCHAR(100),
    
    -- Latest validation accuracy
    accuracy DECIMAL(10, 6),
    
    -- Cumulative GPU-hours invested
    total_compute BIGINT NOT NULL DEFAULT 0,
    
    -- Model status: PROPOSED, ACTIVE, COMPLETED, DEPRECATED
    status VARCHAR(20) NOT NULL DEFAULT 'PROPOSED',
    
    -- License type: OPEN, RESTRICTED, COMMERCIAL
    license_type VARCHAR(20) NOT NULL DEFAULT 'OPEN',
    
    -- Address that proposed this model
    proposer_address BYTEA NOT NULL CHECK (length(proposer_address) = 20),
    
    -- Link to governance proposal
    governance_id BYTEA CHECK (length(governance_id) = 32),
    
    -- Block height when created
    created_at_block BIGINT NOT NULL,
    
    -- Block height of last update
    updated_at_block BIGINT,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for model queries
CREATE INDEX IF NOT EXISTS idx_models_status ON models(status);
CREATE INDEX IF NOT EXISTS idx_models_task_type ON models(task_type);
CREATE INDEX IF NOT EXISTS idx_models_domain ON models(domain);

-----------------------------------
-- MODEL_CONTRIBUTORS TABLE
-----------------------------------
CREATE TABLE IF NOT EXISTS model_contributors (
    model_id BYTEA NOT NULL REFERENCES models(model_id),
    miner_address BYTEA NOT NULL CHECK (length(miner_address) = 20),
    contribution_count BIGINT NOT NULL DEFAULT 0,
    total_quality_score DECIMAL(30, 10) NOT NULL DEFAULT 0,
    PRIMARY KEY (model_id, miner_address)
);

-----------------------------------
-- TASKS TABLE (PoUW Task Queue)
-----------------------------------
CREATE TABLE IF NOT EXISTS tasks (
    -- Unique task identifier
    task_id BYTEA PRIMARY KEY CHECK (length(task_id) = 32),
    
    -- Model this task trains
    model_id BYTEA NOT NULL REFERENCES models(model_id),
    
    -- Batch index for this task
    batch_index BIGINT NOT NULL,
    
    -- Hash of training data batch
    data_hash BYTEA CHECK (length(data_hash) = 32),
    
    -- Hash of model weights to use
    weights_hash BYTEA CHECK (length(weights_hash) = 32),
    
    -- Learning rate for this batch
    learning_rate DECIMAL(10, 8),
    
    -- Difficulty target for this task
    difficulty_target BYTEA NOT NULL,
    
    -- Task status: PENDING, ASSIGNED, COMPLETED, FAILED
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    
    -- Assigned miner (if any)
    assigned_miner BYTEA CHECK (length(assigned_miner) = 20),
    
    -- Block heights
    created_at_block BIGINT NOT NULL,
    assigned_at_block BIGINT,
    completed_at_block BIGINT,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for task queue queries
CREATE INDEX IF NOT EXISTS idx_tasks_model ON tasks(model_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_pending ON tasks(status, created_at_block) WHERE status = 'PENDING';

-----------------------------------
-- PROPOSALS TABLE (Governance)
-----------------------------------
CREATE TABLE IF NOT EXISTS proposals (
    -- Unique proposal identifier
    proposal_id BYTEA PRIMARY KEY CHECK (length(proposal_id) = 32),
    
    -- Proposal type: NEW_MODEL, TASK_PRIORITY, PARAMETER_ADJUST, LICENSE_CHANGE, TREASURY_SPEND, PROTOCOL_UPGRADE
    proposal_type VARCHAR(50) NOT NULL,
    
    -- Proposer's address
    proposer_address BYTEA NOT NULL CHECK (length(proposer_address) = 20),
    
    -- Short title
    title VARCHAR(200) NOT NULL,
    
    -- Full description (markdown)
    description TEXT,
    
    -- Type-specific data (JSON)
    data JSONB,
    
    -- Vote tallies (in voting power units)
    votes_for DECIMAL(30, 0) NOT NULL DEFAULT 0,
    votes_against DECIMAL(30, 0) NOT NULL DEFAULT 0,
    
    -- Thresholds
    quorum_required DECIMAL(5, 4) NOT NULL,
    approval_threshold DECIMAL(5, 4) NOT NULL,
    
    -- Voting period (block heights)
    voting_start_block BIGINT NOT NULL,
    voting_end_block BIGINT NOT NULL,
    
    -- Status: ACTIVE, PASSED, REJECTED, EXECUTED, CANCELLED
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    
    -- Block when executed
    executed_at_block BIGINT,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for proposal queries
CREATE INDEX IF NOT EXISTS idx_proposals_status ON proposals(status);
CREATE INDEX IF NOT EXISTS idx_proposals_type ON proposals(proposal_type);
CREATE INDEX IF NOT EXISTS idx_proposals_voting ON proposals(voting_end_block) WHERE status = 'ACTIVE';

-----------------------------------
-- VOTES TABLE
-----------------------------------
CREATE TABLE IF NOT EXISTS votes (
    proposal_id BYTEA NOT NULL REFERENCES proposals(proposal_id),
    voter_address BYTEA NOT NULL CHECK (length(voter_address) = 20),
    vote_power DECIMAL(30, 0) NOT NULL,
    in_favor BOOLEAN NOT NULL,
    voted_at_block BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (proposal_id, voter_address)
);

-----------------------------------
-- INFERENCE_NODES TABLE (AI Oracle Layer)
-----------------------------------
CREATE TABLE IF NOT EXISTS inference_nodes (
    -- Node operator's address
    address BYTEA PRIMARY KEY CHECK (length(address) = 20),
    
    -- Staked amount (minimum 1000 CCoin)
    staked_amount DECIMAL(30, 0) NOT NULL DEFAULT 0,
    
    -- Hosted model IDs
    hosted_models BYTEA[] NOT NULL DEFAULT '{}',
    
    -- Uptime percentage (0.0 - 1.0)
    uptime DECIMAL(5, 4) NOT NULL DEFAULT 1.0,
    
    -- Query statistics
    total_queries BIGINT NOT NULL DEFAULT 0,
    correct_results BIGINT NOT NULL DEFAULT 0,
    
    -- Last active block
    last_active_block BIGINT,
    
    -- Active status
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-----------------------------------
-- CHAIN_STATE TABLE (Configuration)
-----------------------------------
CREATE TABLE IF NOT EXISTS chain_state (
    key VARCHAR(100) PRIMARY KEY,
    value BYTEA NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Insert initial chain state
INSERT INTO chain_state (key, value) VALUES
    ('genesis_hash', '\x0000000000000000000000000000000000000000000000000000000000000000'),
    ('current_height', '\x0000000000000000'),
    ('current_epoch', '\x0000000000000000'),
    ('total_supply', '\x0000000000000000'),
    ('difficulty', '\x00000000000000000000000000000000ffffffffffffffffffffffffffffffff'),
    ('commitment_tree_root', '\x0000000000000000000000000000000000000000000000000000000000000000'),
    ('commitment_tree_size', '\x0000000000000000')
ON CONFLICT (key) DO NOTHING;

-----------------------------------
-- FUNCTIONS
-----------------------------------

-- Function to update miner timestamp
CREATE OR REPLACE FUNCTION update_miner_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for miner updates
DROP TRIGGER IF EXISTS trigger_update_miner_timestamp ON miners;
CREATE TRIGGER trigger_update_miner_timestamp
    BEFORE UPDATE ON miners
    FOR EACH ROW
    EXECUTE FUNCTION update_miner_timestamp();
