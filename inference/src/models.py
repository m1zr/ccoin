"""
CCoin Inference Layer - Model Management
Handles AI model loading, inference, and gradient computation for PoUW.
"""

import os
import json
import hashlib
from typing import Optional, Dict, Any, List, Tuple
from dataclasses import dataclass, field
from enum import Enum
import asyncio
from pathlib import Path

import torch
import torch.nn as nn
from torch.utils.data import DataLoader, TensorDataset


class ModelStatus(Enum):
    """Model lifecycle states"""
    PROPOSED = "proposed"
    ACTIVE = "active"
    TRAINING = "training"
    COMPLETED = "completed"
    DEPRECATED = "deprecated"


class TaskType(Enum):
    """AI task types"""
    CLASSIFICATION = "classification"
    REGRESSION = "regression"
    GENERATION = "generation"
    REINFORCEMENT = "reinforcement"


@dataclass
class ModelConfig:
    """Model configuration"""
    model_id: str
    architecture: str
    task_type: TaskType
    domain: str
    input_shape: Tuple[int, ...]
    output_shape: Tuple[int, ...]
    hyperparameters: Dict[str, Any] = field(default_factory=dict)


@dataclass
class GradientResult:
    """Result of gradient computation"""
    model_id: str
    batch_start: int
    batch_end: int
    gradient_hash: str
    quality_score: float
    loss: float
    accuracy: float
    compute_time: float


class ModelRegistry:
    """Local model registry for inference layer"""
    
    def __init__(self, models_dir: str = "./models"):
        self.models_dir = Path(models_dir)
        self.models_dir.mkdir(parents=True, exist_ok=True)
        self.models: Dict[str, nn.Module] = {}
        self.configs: Dict[str, ModelConfig] = {}
        
    def register_model(
        self,
        config: ModelConfig,
        model: nn.Module
    ) -> str:
        """Register a model in the local registry"""
        self.models[config.model_id] = model
        self.configs[config.model_id] = config
        
        # Save model and config
        model_path = self.models_dir / f"{config.model_id}.pt"
        config_path = self.models_dir / f"{config.model_id}.json"
        
        torch.save(model.state_dict(), model_path)
        
        with open(config_path, "w") as f:
            json.dump({
                "model_id": config.model_id,
                "architecture": config.architecture,
                "task_type": config.task_type.value,
                "domain": config.domain,
                "input_shape": config.input_shape,
                "output_shape": config.output_shape,
                "hyperparameters": config.hyperparameters,
            }, f, indent=2)
        
        return config.model_id
    
    def load_model(self, model_id: str) -> Optional[nn.Module]:
        """Load a model from the registry"""
        if model_id in self.models:
            return self.models[model_id]
        
        model_path = self.models_dir / f"{model_id}.pt"
        config_path = self.models_dir / f"{model_id}.json"
        
        if not model_path.exists() or not config_path.exists():
            return None
        
        with open(config_path) as f:
            config_data = json.load(f)
        
        config = ModelConfig(
            model_id=config_data["model_id"],
            architecture=config_data["architecture"],
            task_type=TaskType(config_data["task_type"]),
            domain=config_data["domain"],
            input_shape=tuple(config_data["input_shape"]),
            output_shape=tuple(config_data["output_shape"]),
            hyperparameters=config_data.get("hyperparameters", {}),
        )
        
        # Create model based on architecture
        model = self._create_model(config)
        model.load_state_dict(torch.load(model_path))
        
        self.models[model_id] = model
        self.configs[model_id] = config
        
        return model
    
    def _create_model(self, config: ModelConfig) -> nn.Module:
        """Create a model from config"""
        # Simple MLP for demonstration
        if config.architecture == "mlp":
            return SimpleMLP(
                input_size=config.input_shape[0],
                hidden_sizes=config.hyperparameters.get("hidden_sizes", [256, 128]),
                output_size=config.output_shape[0],
            )
        elif config.architecture == "cnn":
            return SimpleCNN(
                input_channels=config.input_shape[0],
                num_classes=config.output_shape[0],
            )
        else:
            raise ValueError(f"Unknown architecture: {config.architecture}")
    
    def list_models(self) -> List[str]:
        """List all registered models"""
        models = []
        for path in self.models_dir.glob("*.json"):
            models.append(path.stem)
        return models
    
    def get_config(self, model_id: str) -> Optional[ModelConfig]:
        """Get model configuration"""
        return self.configs.get(model_id)


class SimpleMLP(nn.Module):
    """Simple MLP for testing"""
    
    def __init__(
        self,
        input_size: int,
        hidden_sizes: List[int],
        output_size: int
    ):
        super().__init__()
        
        layers = []
        prev_size = input_size
        
        for hidden_size in hidden_sizes:
            layers.extend([
                nn.Linear(prev_size, hidden_size),
                nn.ReLU(),
                nn.Dropout(0.1),
            ])
            prev_size = hidden_size
        
        layers.append(nn.Linear(prev_size, output_size))
        self.network = nn.Sequential(*layers)
    
    def forward(self, x: torch.Tensor) -> torch.Tensor:
        return self.network(x)


class SimpleCNN(nn.Module):
    """Simple CNN for testing"""
    
    def __init__(self, input_channels: int, num_classes: int):
        super().__init__()
        
        self.features = nn.Sequential(
            nn.Conv2d(input_channels, 32, 3, padding=1),
            nn.ReLU(),
            nn.MaxPool2d(2),
            nn.Conv2d(32, 64, 3, padding=1),
            nn.ReLU(),
            nn.MaxPool2d(2),
            nn.Conv2d(64, 64, 3, padding=1),
            nn.ReLU(),
        )
        
        self.classifier = nn.Sequential(
            nn.AdaptiveAvgPool2d(1),
            nn.Flatten(),
            nn.Linear(64, num_classes),
        )
    
    def forward(self, x: torch.Tensor) -> torch.Tensor:
        x = self.features(x)
        return self.classifier(x)


def hash_gradients(gradients: Dict[str, torch.Tensor]) -> str:
    """Compute hash of gradients for verification"""
    hasher = hashlib.sha256()
    
    for name in sorted(gradients.keys()):
        grad = gradients[name]
        hasher.update(name.encode())
        hasher.update(grad.cpu().numpy().tobytes())
    
    return hasher.hexdigest()


def compute_gradient_quality(
    gradients: Dict[str, torch.Tensor],
    loss: float,
    prev_loss: Optional[float] = None
) -> float:
    """
    Compute gradient quality score.
    Score is based on:
    - Gradient magnitude (not too small or too large)
    - Loss improvement
    - Gradient diversity
    """
    scores = []
    
    # Magnitude score (prefer gradients in reasonable range)
    total_norm = 0.0
    for grad in gradients.values():
        total_norm += grad.norm().item() ** 2
    total_norm = total_norm ** 0.5
    
    # Ideal norm range: 0.1 to 10
    if 0.1 <= total_norm <= 10:
        magnitude_score = 1.0
    elif total_norm < 0.1:
        magnitude_score = total_norm / 0.1
    else:
        magnitude_score = max(0, 1 - (total_norm - 10) / 100)
    scores.append(magnitude_score)
    
    # Loss improvement score
    if prev_loss is not None and prev_loss > 0:
        improvement = (prev_loss - loss) / prev_loss
        improvement_score = min(1.0, max(0, improvement + 0.5))
        scores.append(improvement_score)
    
    # Average all scores
    return sum(scores) / len(scores)
