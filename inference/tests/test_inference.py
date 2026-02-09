"""
CCoin Inference Layer - Tests for models and gradient computation.
"""

import pytest
import torch
import asyncio
from unittest.mock import MagicMock, AsyncMock

# Import modules under test
from src.models import (
    ModelRegistry,
    ModelConfig,
    TaskType,
    SimpleMLP,
    SimpleCNN,
    hash_gradients,
    compute_gradient_quality,
)
from src.gradient import (
    GradientComputer,
    TrainingTask,
    TaskQueue,
    DatasetLoader,
)


class TestModels:
    """Tests for model components."""

    def test_simple_mlp_creation(self):
        """Test MLP model creation."""
        model = SimpleMLP(
            input_size=784,
            hidden_sizes=[256, 128],
            output_size=10
        )
        
        # Test forward pass
        x = torch.randn(32, 784)
        output = model(x)
        
        assert output.shape == (32, 10)

    def test_simple_cnn_creation(self):
        """Test CNN model creation."""
        model = SimpleCNN(
            input_channels=1,
            num_classes=10
        )
        
        # Test forward pass
        x = torch.randn(32, 1, 28, 28)
        output = model(x)
        
        assert output.shape == (32, 10)

    def test_model_registry(self, tmp_path):
        """Test model registry operations."""
        registry = ModelRegistry(str(tmp_path / "models"))
        
        # Create and register model
        config = ModelConfig(
            model_id="test_model",
            architecture="mlp",
            task_type=TaskType.CLASSIFICATION,
            domain="test",
            input_shape=(784,),
            output_shape=(10,),
        )
        
        model = SimpleMLP(784, [128], 10)
        registry.register_model(config, model)
        
        # List models
        models = registry.list_models()
        assert "test_model" in models
        
        # Load model
        loaded = registry.load_model("test_model")
        assert loaded is not None

    def test_gradient_hashing(self):
        """Test gradient hashing is deterministic."""
        gradients = {
            "layer1.weight": torch.randn(128, 784),
            "layer1.bias": torch.randn(128),
        }
        
        hash1 = hash_gradients(gradients)
        hash2 = hash_gradients(gradients)
        
        assert hash1 == hash2
        assert len(hash1) == 64  # SHA256 hex

    def test_gradient_quality(self):
        """Test gradient quality scoring."""
        # Good gradients (reasonable magnitude)
        good_gradients = {
            "layer1.weight": torch.randn(128, 784) * 0.1,
        }
        
        score = compute_gradient_quality(good_gradients, loss=0.5)
        assert 0 <= score <= 1

        # Very small gradients
        small_gradients = {
            "layer1.weight": torch.randn(128, 784) * 0.0001,
        }
        
        small_score = compute_gradient_quality(small_gradients, loss=0.5)
        assert small_score < score


class TestGradientComputation:
    """Tests for gradient computation."""

    @pytest.fixture
    def registry(self, tmp_path):
        reg = ModelRegistry(str(tmp_path / "models"))
        config = ModelConfig(
            model_id="test_model",
            architecture="mlp",
            task_type=TaskType.CLASSIFICATION,
            domain="test",
            input_shape=(784,),
            output_shape=(10,),
        )
        model = SimpleMLP(784, [128], 10)
        reg.register_model(config, model)
        return reg

    @pytest.fixture
    def computer(self, registry):
        return GradientComputer(registry, device="cpu")

    @pytest.mark.asyncio
    async def test_compute_gradients(self, computer):
        """Test gradient computation."""
        task = TrainingTask(
            task_id="task_001",
            model_id="test_model",
            dataset_cid="ipfs://test",
            batch_start=0,
            batch_end=32,
            objective_hash="abc123",
            deadline=999999,
            reward=1000,
        )
        
        # Generate test data
        data = torch.randn(100, 784)
        targets = torch.randint(0, 10, (100,))
        
        result = await computer.compute_gradients(task, data, targets)
        
        assert result.task_id == "task_001"
        assert len(result.gradient_hash) == 64
        assert 0 <= result.quality_score <= 1
        assert result.loss > 0


class TestTaskQueue:
    """Tests for task queue."""

    @pytest.mark.asyncio
    async def test_add_and_claim_task(self):
        """Test adding and claiming tasks."""
        queue = TaskQueue("http://localhost:8545")
        
        task = TrainingTask(
            task_id="task_001",
            model_id="model_001",
            dataset_cid="ipfs://test",
            batch_start=0,
            batch_end=100,
            objective_hash="abc",
            deadline=999999,
            reward=1000,
        )
        
        queue.add_task(task)
        
        # Fetch tasks
        tasks = await queue.fetch_tasks()
        assert len(tasks) == 1
        
        # Claim task
        claimed = await queue.claim_task("task_001")
        assert claimed is not None
        assert claimed.task_id == "task_001"
        
        # Task should no longer be pending
        tasks = await queue.fetch_tasks()
        assert len(tasks) == 0


class TestDatasetLoader:
    """Tests for dataset loading."""

    @pytest.mark.asyncio
    async def test_load_synthetic_dataset(self):
        """Test loading synthetic dataset."""
        loader = DatasetLoader()
        
        data, targets = await loader.load_dataset("ipfs://synthetic")
        
        assert data.shape[0] == 1000
        assert data.shape[1] == 784
        assert targets.shape[0] == 1000

    def test_get_batch(self):
        """Test batch extraction."""
        loader = DatasetLoader()
        
        data = torch.randn(100, 784)
        targets = torch.randint(0, 10, (100,))
        
        batch_data, batch_targets = loader.get_batch(data, targets, 10, 20)
        
        assert batch_data.shape[0] == 10
        assert batch_targets.shape[0] == 10


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
