#!/usr/bin/env python3
"""Semantic memory search sidecar using mem0 with Ollama embeddings.

Exposes POST /search with {"query": "...", "user_id": "...", "limit": 10}
Returns [{"id": "...", "memory": "...", "score": 0.95}, ...]

Configure via environment variables:
  QDRANT_HOST, QDRANT_PORT  — Qdrant server (default: localhost:6333)
  OLLAMA_BASE_URL           — Ollama server (default: http://localhost:11434)
  LLM_MODEL                 — LLM model (default: llama3.1:latest)
  EMBEDDER_MODEL            — Embedding model (default: nomic-embed-text)
  EMBEDDER_DIMENSIONS       — Embedding dimensions (default: 768)
  SEARCH_PORT               — Listen port (default: 9092)
  COLLECTION_NAME           — Qdrant collection (default: openmemory)
"""

import json
import os
from http.server import HTTPServer, BaseHTTPRequestHandler

from mem0 import Memory


def get_config():
    base_url = os.environ.get("OLLAMA_BASE_URL", "http://localhost:11434")
    qdrant_host = os.environ.get("QDRANT_HOST", "localhost")
    qdrant_port = int(os.environ.get("QDRANT_PORT", "6333"))
    collection = os.environ.get("COLLECTION_NAME", "openmemory")

    embedder_dims = int(os.environ.get("EMBEDDER_DIMENSIONS", "768"))

    return {
        "vector_store": {
            "provider": "qdrant",
            "config": {
                "collection_name": collection,
                "host": qdrant_host,
                "port": qdrant_port,
                "embedding_model_dims": embedder_dims,
            },
        },
        "llm": {
            "provider": "ollama",
            "config": {
                "model": os.environ.get("LLM_MODEL", "llama3.1:latest"),
                "ollama_base_url": base_url,
            },
        },
        "embedder": {
            "provider": "ollama",
            "config": {
                "model": os.environ.get("EMBEDDER_MODEL", "nomic-embed-text"),
                "ollama_base_url": base_url,
            },
        },
        "version": "v1.1",
    }


class SearchHandler(BaseHTTPRequestHandler):
    memory_client = None

    def log_message(self, format, *args):
        pass  # suppress access logs

    def do_POST(self):
        if self.path != "/search":
            self.send_error(404)
            return

        content_length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(content_length))

        query = body.get("query", "")
        user_id = body.get("user_id", "default_user")
        limit = body.get("limit", 10)
        threshold = body.get("threshold", 0.1)

        if not query:
            self.send_error(400, "missing query")
            return

        try:
            results = self.memory_client.search(
                query,
                top_k=limit,
                filters={"user_id": user_id},
                threshold=threshold,
            )
        except Exception as e:
            self.send_error(500, str(e))
            return

        items = []
        for r in results.get("results", []):
            items.append({
                "id": r.get("id", ""),
                "memory": r.get("memory", ""),
                "score": round(r.get("score", 0.0), 4),
            })

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(items).encode())

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')
            return
        self.send_error(404)


def main():
    print("Initializing mem0 client...")
    config = get_config()
    print(f"  Qdrant: {config['vector_store']['config']['host']}:{config['vector_store']['config']['port']}")
    print(f"  Ollama: {config['embedder']['config']['ollama_base_url']}")
    print(f"  Embedder: {config['embedder']['config']['model']} ({config['vector_store']['config']['embedding_model_dims']}d)")
    print(f"  Collection: {config['vector_store']['config']['collection_name']}")

    SearchHandler.memory_client = Memory.from_config(config_dict=config)
    print("mem0 client initialized.")

    port = int(os.environ.get("SEARCH_PORT", "9092"))
    server = HTTPServer(("127.0.0.1", port), SearchHandler)
    print(f"Listening on 127.0.0.1:{port}")
    server.serve_forever()


if __name__ == "__main__":
    main()
