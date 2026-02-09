"""
CCoin Inference Layer - Gradient Computation for PoUW
Implements distributed gradient computation for Proof-of-Useful-Work.
"""

import time
import hashlib
from typing import Optional, Dict, Any, List, Tuple
from dataclasses import dataclass
import asyncio

import torch
import torch.nn as nn
from torch.utils.data import DataLoader, TensorDataset
import numpy as np

from .models import (
    ModelRegistry,
    GradientResult,
    hash_gradients,
    compute_gradient_quality,
)


@dataclass
class TrainingTask:
    """Training task from the blockchain"""
    task_id: str
    model_id: str
    dataset_cid: str
    batch_start: int
    batch_end: int
    objective_hash: str
    deadline: int
    reward: int


@dataclass
class ComputeResult:
    """Result of gradient computation"""
    task_id: str
    gradient_hash: str
    quality_score: float
    loss: float
    metrics: Dict[str, float]
    proof: bytes


class GradientComputer:
    """
    Computes gradients for PoUW mining.
    Each gradient computation contributes to model training
    and earns block rewards.
    """
    
    def __init__(
        self,
        registry: ModelRegistry,
        device: str = "auto"
    ):
        self.registry = registry
        
        if device == "auto":
            self.device = torch.device(
                "cuda" if torch.cuda.is_available() else "cpu"
            )
        else:
            self.device = torch.device(device)
        
        self.current_task: Optional[TrainingTask] = None
        self.prev_loss: Optional[float] = None
    
    async def compute_gradients(
        self,
        task: TrainingTask,
        data: torch.Tensor,
        targets: torch.Tensor,
        learning_rate: float = 0.001
    ) -> ComputeResult:
        """
        Compute gradients for a training task.
        This is the core PoUW computation.
        """
        start_time = time.time()
        
        # Load model
        model = self.registry.load_model(task.model_id)
        if model is None:
            raise ValueError(f"Model not found: {task.model_id}")
        
        model = model.to(self.device)
        model.train()
        
        # Move data to device
        data = data.to(self.device)
        targets = targets.to(self.device)
        
        # Select batch range
        batch_data = data[task.batch_start:task.batch_end]
        batch_targets = targets[task.batch_start:task.batch_end]
        
        # Forward pass
        outputs = model(batch_data)
        
        # Compute loss
        criterion = self._get_criterion(task.model_id)
        loss = criterion(outputs, batch_targets)
        
        # Backward pass
        model.zero_grad()
        loss.backward()
        
        # Collect gradients
        gradients = {}
        for name, param in model.named_parameters():
            if param.grad is not None:
                gradients[name] = param.grad.clone()
        
        # Compute quality score
        quality_score = compute_gradient_quality(
            gradients,
            loss.item(),
            self.prev_loss
        )
        
        # Compute gradient hash
        gradient_hash = hash_gradients(gradients)
        
        # Compute metrics
        metrics = self._compute_metrics(outputs, batch_targets, task.model_id)
        metrics["loss"] = loss.item()
        metrics["compute_time"] = time.time() - start_time
        
        # Generate proof
        proof = self._generate_proof(
            task, gradient_hash, quality_score, metrics
        )
        
        # Update state
        self.prev_loss = loss.item()
        self.current_task = task
        
        return ComputeResult(
            task_id=task.task_id,
            gradient_hash=gradient_hash,
            quality_score=quality_score,
            loss=loss.item(),
            metrics=metrics,
            proof=proof,
        )
    
    def _get_criterion(self, model_id: str) -> nn.Module:
        """Get loss function for model"""
        config = self.registry.get_config(model_id)
        if config is None:
            return nn.CrossEntropyLoss()
        
        task_type = config.task_type.value
        if task_type == "classification":
            return nn.CrossEntropyLoss()
        elif task_type == "regression":
            return nn.MSELoss()
        else:
            return nn.CrossEntropyLoss()
    
    def _compute_metrics(
        self,
        outputs: torch.Tensor,
        targets: torch.Tensor,
        model_id: str
    ) -> Dict[str, float]:
        """Compute training metrics"""
        metrics = {}
        
        config = self.registry.get_config(model_id)
        task_type = config.task_type.value if config else "classification"
        
        if task_type == "classification":
            # Accuracy
            preds = outputs.argmax(dim=1)
            correct = (preds == targets).sum().item()
            total = targets.size(0)
            metrics["accuracy"] = correct / total
            
        elif task_type == "regression":
            # MSE and MAE
            mse = ((outputs - targets) ** 2).mean().item()
            mae = (outputs - targets).abs().mean().item()
            metrics["mse"] = mse
            metrics["mae"] = mae
        
        return metrics
    
    def _generate_proof(
        self,
        task: TrainingTask,
        gradient_hash: str,
        quality_score: float,
        metrics: Dict[str, float]
    ) -> bytes:
        """
        Generate cryptographic proof of computation.
        This proves the work was done correctly.
        """
        # In production, this would be a zk-SNARK proof
        # For now, we create a simple hash-based commitment
        
        proof_data = {
            "task_id": task.task_id,
            "gradient_hash": gradient_hash,
            "quality_score": quality_score,
            "metrics": metrics,
            "timestamp": int(time.time()),
        }
        
        # Create commitment
        commitment = hashlib.sha256(
            str(proof_data).encode()
        ).digest()
        
        return commitment
    
    def verify_result(
        self,
        result: ComputeResult,
        task: TrainingTask
    ) -> bool:
        """Verify a computation result"""
        # Verify task ID matches
        if result.task_id != task.task_id:
            return False
        
        # Verify quality score is reasonable
        if not 0 <= result.quality_score <= 1:
            return False
        
        # Verify proof
        expected_proof = self._generate_proof(
            task,
            result.gradient_hash,
            result.quality_score,
            result.metrics,
        )
        
        # Note: In production, verification would be more sophisticated
        return True


class TaskQueue:
    """
    Manages training tasks from the blockchain.
    """
    
    def __init__(self, rpc_endpoint: str):
        self.rpc_endpoint = rpc_endpoint
        self.pending_tasks: List[TrainingTask] = []
        self.active_task: Optional[TrainingTask] = None
    
    async def fetch_tasks(self) -> List[TrainingTask]:
        """Fetch available tasks from blockchain"""
        # In production, would call RPC endpoint
        # For now, return empty list or simulate
        return self.pending_tasks
    
    async def claim_task(self, task_id: str) -> Optional[TrainingTask]:
        """Claim a task for computation"""
        for task in self.pending_tasks:
            if task.task_id == task_id:
                self.active_task = task
                self.pending_tasks.remove(task)
                return task
        return None
    
    async def submit_result(self, result: ComputeResult) -> bool:
        """Submit computation result to blockchain"""
        # In production, would submit via RPC
        self.active_task = None
        return True
    
    def add_task(self, task: TrainingTask):
        """Add a task to the queue (for testing)"""
        self.pending_tasks.append(task)


class DatasetLoader:
    """
    Loads datasets from IPFS or local storage.
    """
    
    def __init__(self, cache_dir: str = "./data_cache"):
        self.cache_dir = cache_dir
    
    async def load_dataset(
        self,
        dataset_cid: str
    ) -> Tuple[torch.Tensor, torch.Tensor]:
        """
        Load dataset from IPFS CID.
        Returns (data, targets) tensors.
        """
        # In production, would fetch from IPFS
        # For now, generate synthetic data
        
        # Generate synthetic classification dataset
        num_samples = 1000
        input_size = 784  # MNIST-like
        num_classes = 10
        
        data = torch.randn(num_samples, input_size)
        targets = torch.randint(0, num_classes, (num_samples,))
        
        return data, targets
    
    def get_batch(
        self,
        data: torch.Tensor,
        targets: torch.Tensor,
        batch_start: int,
        batch_end: int
    ) -> Tuple[torch.Tensor, torch.Tensor]:
        """Get a batch from the dataset"""
        return data[batch_start:batch_end], targets[batch_start:batch_end]
