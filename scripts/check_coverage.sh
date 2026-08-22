#!/bin/bash
# Copyright 2026 [Copyright Holder]
# Licensed under the Apache License, Version 2.0 (the "License");

set -e

# 全 internal パッケージのテスト実行とカバレッジプロファイルの出力
echo "==> Running tests with coverage profile..."
go test -v -race -coverprofile=coverage.out ./internal/...

# internal 配下の合計ステートメントカバー率を検証
echo "==> Verifying business logic coverage (internal/collector, internal/state, internal/orchestrator, internal/gateway, internal/common)..."
awk '
BEGIN { total = 0; covered = 0; }
/:/ {
    if ($0 ~ /\/internal\/(collector|state|orchestrator|gateway|common)\//) {
        total += $2;
        if ($3 > 0) {
            covered += $2;
        }
    }
}
END {
    if (total == 0) {
        printf "No business logic statements found in this layer yet (schema/database phase). Skipping percentage threshold.\n"
        exit 0
    }
    rate = (covered / total) * 100
    printf "=========================================\n"
    printf "Business Logic Coverage Summary:\n"
    printf "  Covered Statements: %d\n", covered
    printf "  Total Statements:   %d\n", total
    printf "  Coverage Rate:      %.2f%%\n", rate
    printf "=========================================\n"
    if (rate < 80.0) {
        printf "ERROR: Business logic coverage is %.2f%%, which is below the required 80.0%%!\n", rate
        exit 1
    }
    printf "SUCCESS: Business logic coverage is %.2f%% (>= 80.0%%)\n", rate
}
' coverage.out
