# @blackwell-systems/gcf-cli

CLI for [GCF (Graph Compact Format)](https://github.com/blackwell-systems/gcf): token-optimized wire format for LLM tool responses.

## Install

```bash
npm install -g @blackwell-systems/gcf-cli
```

## Usage

```bash
gcf encode < payload.json    # JSON to GCF
gcf decode < payload.gcf     # GCF to JSON
gcf stats  < payload.json    # token comparison with visual bar
```

## What is GCF?

84% fewer tokens than JSON, 34% fewer than TOON, 100% LLM comprehension accuracy at scale.

Specification: https://github.com/blackwell-systems/gcf
