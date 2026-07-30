# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [1.0.0] - 2026-07-30

First public, open-source release.

- Reads heat meters (M-Bus/serial), grid meters (DSMR P1/serial), solar production
  (Enphase Envoy HTTP API), and ventilation data (DucoBox HTTP API).
- Stores readings to QuestDB, Postgres, MySQL, ClickHouse, TDEngine, TimescaleDB, or stdout,
  with all enabled sinks written to concurrently.
- Ports-and-adapters architecture: domain core has no IO dependencies, adapters implement
  domain interfaces for each source and sink.
- Exposes Prometheus metrics and `/healthz`/`/readyz` HTTP endpoints for liveness and readiness.
