"""
CCoin Inference Layer - FastAPI Server
REST API for inference, gradient computation, and model management.
"""

import os
import asyncio
from typing import Optional, Dict, Any, List
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, BackgroundTasks
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
import torch
import uvicorn

from .models import ModelRegistry, ModelConfig, TaskType, SimpleMLP
from .gradient import (
    GradientComputer,
    TrainingTask,
    TaskQueue,
    DatasetLoader,
)


# Request/Response models

class RegisterModelRequest(BaseModel):
    model_id: str
    architecture: str
    task_type: str
    domain: str
    input_shape: List[int]
    output_shape: List[int]
    hyperparameters: Dict[str, Any] = {}


class ComputeGradientRequest(BaseModel):
    task_id: str
    model_id: str
    dataset_cid: str
    batch_start: int
    batch_end: int
    objective_hash: str
    deadline: int
    reward: int


class ComputeGradientResponse(BaseModel):
    task_id: str
    gradient_hash: str
    quality_score: float
    loss: float
    metrics: Dict[str, float]
    proof: str


class InferenceRequest(BaseModel):
    model_id: str
    input_data: List[List[float]]


class InferenceResponse(BaseModel):
    model_id: str
    predictions: List[Any]
    latency_ms: float


class ModelInfoResponse(BaseModel):
    model_id: str
    architecture: str
    task_type: str
    domain: str
    input_shape: List[int]
    output_shape: List[int]


class HealthResponse(BaseModel):
    status: str
    gpu_available: bool
    models_loaded: int
    pending_tasks: int


# Global state
registry: Optional[ModelRegistry] = None
computer: Optional[GradientComputer] = None
task_queue: Optional[TaskQueue] = None
dataset_loader: Optional[DatasetLoader] = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Initialize services on startup"""
    global registry, computer, task_queue, dataset_loader
    
    models_dir = os.getenv("MODELS_DIR", "./models")
    rpc_endpoint = os.getenv("RPC_ENDPOINT", "http://localhost:8545")
    
    registry = ModelRegistry(models_dir)
    computer = GradientComputer(registry)
    task_queue = TaskQueue(rpc_endpoint)
    dataset_loader = DatasetLoader()
    
    # Register a test model
    if not registry.list_models():
        test_config = ModelConfig(
            model_id="test_mlp",
            architecture="mlp",
            task_type=TaskType.CLASSIFICATION,
            domain="test",
            input_shape=(784,),
            output_shape=(10,),
            hyperparameters={"hidden_sizes": [256, 128]},
        )
        test_model = SimpleMLP(784, [256, 128], 10)
        registry.register_model(test_config, test_model)
    
    yield
    
    # Cleanup
    pass


# Create FastAPI app
app = FastAPI(
    title="CCoin Inference Layer",
    description="AI model inference and gradient computation for PoUW",
    version="1.0.0",
    lifespan=lifespan,
)

# CORS
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


# Endpoints

@app.get("/health", response_model=HealthResponse)
async def health_check():
    """Health check endpoint"""
    return HealthResponse(
        status="healthy",
        gpu_available=torch.cuda.is_available(),
        models_loaded=len(registry.models) if registry else 0,
        pending_tasks=len(task_queue.pending_tasks) if task_queue else 0,
    )


@app.get("/models", response_model=List[str])
async def list_models():
    """List all registered models"""
    if not registry:
        raise HTTPException(500, "Registry not initialized")
    return registry.list_models()


@app.get("/models/{model_id}", response_model=ModelInfoResponse)
async def get_model(model_id: str):
    """Get model information"""
    if not registry:
        raise HTTPException(500, "Registry not initialized")
    
    config = registry.get_config(model_id)
    if not config:
        # Try loading
        model = registry.load_model(model_id)
        if not model:
            raise HTTPException(404, f"Model not found: {model_id}")
        config = registry.get_config(model_id)
    
    return ModelInfoResponse(
        model_id=config.model_id,
        architecture=config.architecture,
        task_type=config.task_type.value,
        domain=config.domain,
        input_shape=list(config.input_shape),
        output_shape=list(config.output_shape),
    )


@app.post("/models/register")
async def register_model(request: RegisterModelRequest):
    """Register a new model"""
    if not registry:
        raise HTTPException(500, "Registry not initialized")
    
    config = ModelConfig(
        model_id=request.model_id,
        architecture=request.architecture,
        task_type=TaskType(request.task_type),
        domain=request.domain,
        input_shape=tuple(request.input_shape),
        output_shape=tuple(request.output_shape),
        hyperparameters=request.hyperparameters,
    )
    
    # Create model
    model = registry._create_model(config)
    registry.register_model(config, model)
    
    return {"status": "registered", "model_id": request.model_id}


@app.post("/inference", response_model=InferenceResponse)
async def run_inference(request: InferenceRequest):
    """Run inference on a model"""
    if not registry:
        raise HTTPException(500, "Registry not initialized")
    
    model = registry.load_model(request.model_id)
    if not model:
        raise HTTPException(404, f"Model not found: {request.model_id}")
    
    import time
    start = time.time()
    
    # Prepare input
    input_tensor = torch.tensor(request.input_data, dtype=torch.float32)
    
    # Run inference
    model.eval()
    with torch.no_grad():
        output = model(input_tensor)
    
    latency = (time.time() - start) * 1000
    
    # Convert output
    if output.dim() > 1 and output.size(1) > 1:
        predictions = output.argmax(dim=1).tolist()
    else:
        predictions = output.squeeze().tolist()
    
    return InferenceResponse(
        model_id=request.model_id,
        predictions=predictions,
        latency_ms=latency,
    )


@app.post("/gradients/compute", response_model=ComputeGradientResponse)
async def compute_gradients(request: ComputeGradientRequest):
    """Compute gradients for a training task"""
    if not computer or not dataset_loader:
        raise HTTPException(500, "Services not initialized")
    
    task = TrainingTask(
        task_id=request.task_id,
        model_id=request.model_id,
        dataset_cid=request.dataset_cid,
        batch_start=request.batch_start,
        batch_end=request.batch_end,
        objective_hash=request.objective_hash,
        deadline=request.deadline,
        reward=request.reward,
    )
    
    # Load dataset
    data, targets = await dataset_loader.load_dataset(request.dataset_cid)
    
    # Compute gradients
    result = await computer.compute_gradients(task, data, targets)
    
    return ComputeGradientResponse(
        task_id=result.task_id,
        gradient_hash=result.gradient_hash,
        quality_score=result.quality_score,
        loss=result.loss,
        metrics=result.metrics,
        proof=result.proof.hex(),
    )


@app.get("/tasks")
async def get_pending_tasks():
    """Get pending training tasks"""
    if not task_queue:
        raise HTTPException(500, "Task queue not initialized")
    return [
        {
            "task_id": t.task_id,
            "model_id": t.model_id,
            "batch_start": t.batch_start,
            "batch_end": t.batch_end,
            "reward": t.reward,
        }
        for t in task_queue.pending_tasks
    ]


@app.post("/tasks/{task_id}/claim")
async def claim_task(task_id: str):
    """Claim a task for computation"""
    if not task_queue:
        raise HTTPException(500, "Task queue not initialized")
    
    task = await task_queue.claim_task(task_id)
    if not task:
        raise HTTPException(404, f"Task not found: {task_id}")
    
    return {"status": "claimed", "task_id": task_id}


def main():
    """Run the server"""
    host = os.getenv("HOST", "0.0.0.0")
    port = int(os.getenv("PORT", "8000"))
    
    uvicorn.run(
        "src.server:app",
        host=host,
        port=port,
        reload=True,
    )


if __name__ == "__main__":
    main()
