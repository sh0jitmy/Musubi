# Musubi (結び) - Air-gapped SNMP Scenario Orchestrator

[![CI](https://github.com/sh0jitmy/Musubi/actions/workflows/ci.yml/badge.svg)](https://github.com/sh0jitmy/Musubi/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sh0jitmy/Musubi)](https://goreportcard.com/report/github.com/sh0jitmy/Musubi)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![OpenAPI 3.1](https://img.shields.io/badge/OpenAPI-3.1.0-green.svg)](api/openapi.yaml)

**Musubi (結び)** は、エアギャップ（完全隔離）環境での自律稼働を前提に設計された、**高信頼・高性能なイベント駆動型 SNMP ネットワーク自動試験・状態監視プラットフォーム**です。

複数の SNMP Agent（ルータ、スイッチ、サーバー等のネットワーク機器）に対して、宣言的な YAML シナリオに基づく自動試験、SNMP Trap / Inform 連動のリアルタイム検証、CEL (Common Expression Language) による型安全な状態評価、排他リースロックによる機器保護を提供します。

---

## 🌟 主な特徴 (Key Features)

* 🔒 **Air-gapped ＆ ゼロ外部依存設計**: インターネット接続のない隔離ラボ環境でも、単一バイナリおよび Docker Compose で完結稼働。
* ⚡ **ミリ秒級のイベント駆動アーキテクチャ**: ポーリング待ちによるタイムラグを排し、SNMP Trap / Inform 受信と同時に CEL 条件評価を実行してシナリオを即座に進捗。
* 📐 **CEL (Common Expression Language) による宣言的評価**: `raw['spine1']['IF-MIB::ifOperStatus.1'] == 'up'` のような直感的で高速・型安全な条件判定。
* 🛡️ **Lease Lock によるターゲット保護**: 複数エンジニアや並行テストによるターゲット機器の競合や誤設定変更を自動でブロック。
* 📊 **統合オブザーバビリティ (VictoriaMetrics & Grafana)**: CPU・メモリ・帯域・SNMP テレメトリ・MIB テーブル・Trap ログをリアルタイム可視化する公式ダッシュボードを同梱。
* 🧩 **OpenAPI 3.1 & REST / SSE API 完備**: 全機能を標準化された RESTful API および SSE (Server-Sent Events) ストリームで外部公開。CLI (`musubi-cli`) も標準提供。

---

## 🏗️ システムアーキテクチャ (Architecture Overview)

Musubi は「モジュラーモノリス (Modular Monolith) ＆ マイクロサービス対応」アーキテクチャを採用しており、高スループットな非同期処理と明確なドメイン境界を備えています。

```mermaid
graph TB
    subgraph Clients["Clients & Visualization Layer"]
        CLI["Musubi CLI (musubi-cli)"]
        WebUI["Web Browser / REST Client"]
        Grafana["Grafana Dashboard (:3000)"]
    end

    subgraph MusubiServer["Musubi Core Engine (musubi-server :8080)"]
        Gateway["REST API & SSE Gateway\n(Gin / OpenAPI 3.1)"]
        Orchestrator["Scenario Orchestrator\n(DAG Engine & Pre-flight)"]
        CELEval["CEL Evaluator\n(State Condition Matcher)"]
        StateStore["State Repository\n(Raw & Derived State)"]
        LifecycleMgr["Lifecycle & Lease Lock\n(Target In-Use Protection)"]
        Collector["SNMP Collector & Listener\n(UDP 162 Trap/Inform & Poller)"]
        NotifHub["Notification Hub\n(Ring-buffer Event Streamer)"]
    end

    subgraph StorageLayer["Data & Telemetry Storage"]
        EntDB[("PostgreSQL / SQLite\n(Ent ORM Persistence)")]
        TSDB["VictoriaMetrics (:8428)\n(Prometheus-compatible TSDB)"]
    end

    subgraph NetworkTargets["Network Devices & Test Target Sets"]
        Device1["Spine / Leaf Switches"]
        Device2["Routers & Firewalls"]
        MockAgent["Pure-Go Mock SNMP Agent (:161)"]
    end

    %% Flow connections
    CLI --> Gateway
    WebUI --> Gateway
    Grafana --> TSDB
    Grafana --> Gateway

    Gateway --> Orchestrator
    Gateway --> StateStore
    Gateway --> LifecycleMgr
    Gateway --> NotifHub

    Orchestrator --> CELEval
    Orchestrator --> LifecycleMgr
    Orchestrator --> StateStore
    Orchestrator --> Collector

    Collector --> StateStore
    Collector --> NotifHub
    Collector <--> NetworkTargets

    Gateway --> EntDB
    Gateway --> TSDB
```

---

## 📁 リポジトリ構成 (Repository Structure)

```
Musubi/
├── api/
│   └── openapi.yaml               # OpenAPI 3.1 API 完全定義仕様書
├── cmd/
│   ├── musubi-server/             # Musubi メインサーバーバイナリ
│   ├── musubi-cli/                # 運用・自動化用 CLI ツール
│   └── mock-snmp-agent/           # テスト・検証用 Pure-Go Mock SNMP エージェント
├── deploy/
│   ├── grafana/                   # Grafana プロビジョニング設定 & 監視ダッシュボード
│   └── victoriametrics/           # VictoriaMetrics (TSDB) 設定
├── docs/
│   ├── user_manual.md             # 📖 エンドユーザー向け完全利用マニュアル
│   ├── architecture.md            # 🏛️ 詳細アーキテクチャ設計仕様書
│   ├── maintenance_guide.md       # 🔧 メンテナー向け保守・トラブルシューティングガイド
│   └── adr/                       # アーキテクチャ決定記録 (ADR)
├── ent/                           # Ent ORM スキーマ定義 & 自動生成コード
├── examples/                      # 実践サンプルシナリオ (8+リクエスト & Inform待ち等)
│   └── scenarios/                 # adhoc_8step_linkdown_recovery.yaml 等
├── internal/
│   ├── collector/                 # SNMP Trap/Inform リスナー & SNMP クライアント
│   ├── common/                    # 共通基盤 (errors, batcher, lifecycle, telemetry, notification)
│   ├── database/                  # DB 接続 & 初期シード管理
│   ├── gateway/                   # REST API ルーター & ミドルウェア
│   ├── orchestrator/              # シナリオパーサー, プリフライト検証, ジョブランナー
│   ├── state/                     # CEL 評価エンジン & 状態リポジトリ
│   └── testutil/                  # テスト用ユーティリティ & Mock SNMP Agent
├── scripts/
│   ├── demo.sh                    # フルスタック動作確認用ワンコマンドデモ
│   └── docker_e2e.sh              # Docker Compose E2E 自動テストスイート
├── docker-compose.yml             # コンテナ一括起動用 Docker Compose 定義
├── Dockerfile                     # マルチステージビルド Dockerfile
├── Makefile                       # ビルド・テスト・静的解析自動化 Makefile
├── REQUIREMENTS.md                # 要件チェックリスト & 自己評価レポート
└── README.md                      # 本ドキュメント
```

---

## 🚀 クイックスタート (Quick Start)

### 1. Docker Compose での一括起動 (推奨)

```bash
# スタック全体のビルド & 起動 (Musubi, Mock Agent, VictoriaMetrics, Grafana)
docker compose up -d --build

# 起動状態のヘルスチェック
curl -s http://localhost:8080/v1/system/healths | jq .
```

* 🌐 **REST API & Swagger/OpenAPI**: `http://localhost:8080/v1/system/healthz`
* 📊 **Grafana ダッシュボード**: `http://localhost:3000/d/musubi-overview`
* 📈 **VictoriaMetrics TSDB**: `http://localhost:8428`

---

### 2. ワンコマンド デモスクリプトの実行

スタックの起動から、認証プロファイル作成、ターゲット登録、シナリオ登録、ジョブ実行までを完全自動で体験できます：

```bash
./scripts/demo.sh
```

---

### 3. ローカルバイナリのビルドと起動

```bash
# 全バイナリを bin/ に一括ビルド
make build

# サーバーの起動 (ポート 8080, UDP 162 でリスン)
./bin/musubi-server

# CLI ヘルプの確認
./bin/musubi-cli --help
```

---

## 💡 基本的な使い方 (Basic Usage Workflow)

```mermaid
sequenceDiagram
    autonumber
    actor User as オペレーター / CI
    participant CLI as musubi-cli
    participant API as musubi-server
    participant Device as ネットワーク機器 (SNMP)

    Note over User,API: 1. 準備フェーズ
    User->>CLI: credentials create (SNMP v3認証情報)
    CLI->>API: POST /v1/credentials
    User->>CLI: targets create (機器IP・ポート・ラベル)
    CLI->>API: POST /v1/targets
    User->>CLI: scenarios import -f scenario.yaml
    CLI->>API: POST /v1/scenarios

    Note over User,Device: 2. 試験実行フェーズ
    User->>CLI: scenarios run --name bgp-check
    CLI->>API: POST /v1/scenarios/{name}/runs
    API->>API: ターゲット排他ロック獲得 (Lease Lock)
    API->>Device: SNMP GET / SET 実行
    Device-->>API: SNMP Trap / Inform 送信
    API->>API: CEL 式による状態評価 (wait.until)
    API-->>User: SSE リアルタイム進捗通知 / Grafana 可視化
    API->>API: ロック解放 & Teardown 実行 (完了)
```

### ステップ 1: 認証プロファイル & ターゲット登録
```bash
# SNMP v3 認証プロファイル作成
./bin/musubi-cli credentials create \
  --name "v3-admin" \
  --version "v3" \
  --sec-level "authPriv" \
  --username "admin" \
  --auth-proto "SHA256" --auth-pass "AuthPass123!" \
  --priv-proto "AES"    --priv-pass "PrivPass123!"

# ターゲット機器登録
./bin/musubi-cli targets create \
  --name "spine1" \
  --host "192.168.10.1" \
  --port 161 \
  --credential "v3-admin" \
  --labels "role=spine,site=dc1"
```

### ステップ 2: シナリオ YAML の作成と登録
```yaml
# scenarios/linkdown.yaml
name: "spine1-failover-test"
target_locks: ["spine1"]
steps:
  - id: "check_link_up"
    target: "spine1"
    wait:
      until: "raw['spine1']['IF-MIB::ifOperStatus.1'] == 'up'"
      timeout: "5s"
  - id: "inject_link_down"
    target: "spine1"
    action: "action.snmp_set"
    params:
      oid: "1.3.6.1.2.1.2.2.1.7.1"
      type: "int"
      value: 2
teardown:
  - id: "restore_link"
    target: "spine1"
    action: "action.snmp_set"
    params:
      oid: "1.3.6.1.2.1.2.2.1.7.1"
      type: "int"
      value: 1
```

```bash
# シナリオのインポート
./bin/musubi-cli scenarios import --file ./scenarios/linkdown.yaml
```

### ステップ 3: シナリオ実行 & 進捗確認
```bash
# シナリオの実行
./bin/musubi-cli scenarios run --name "spine1-failover-test"

# ジョブのステータス & ログ確認
./bin/musubi-cli jobs status --id "<JOB_ID>"
./bin/musubi-cli jobs logs --id "<JOB_ID>"
```

### ステップ 4: ワンショット・オンデマンド シナリオ直接実行 (Ad-hoc Execution)
繰り返されない1回限りのテストやCI/CDからのオンデマンド試験では、**シナリオ登録と実行を分けずに1度のAPI呼び出しで直接実行** できます。シナリオ一覧を汚染せず、各種ログ（Job履歴、状態遷移ログ、監査ログ）を漏れなく自動記録します。

```bash
# 8リクエスト以上 & SNMP Inform-Request 受信待ちを含むサンプルシナリオの同期ワンショット実行
curl -X POST http://localhost:8080/v1/scenarios/adhoc \
  -H "Content-Type: application/json" \
  -d '{
    "name": "adhoc-8step-linkdown-recovery",
    "wait": true,
    "inputs": { "target": "spine1" },
    "dsl_yaml": "'"$(cat examples/scenarios/adhoc_8step_linkdown_recovery.yaml | sed 's/"/\\"/g' | awk '{printf "%s\\n", $0}')"'"
  }'
```
**レスポンス (`200 OK`):**
```json
{
  "job_id": "job-adhoc-1757112000000000",
  "scenario_id": "adhoc-8step-linkdown-recovery",
  "status": "SUCCESS",
  "duration_ms": 135,
  "locked_targets": ["spine1"],
  "stream_url": "/v1/events/streams?topics=job.step_advanced&job_id=job-adhoc-1757112000000000"
}
```

---

## 📊 Grafana モニタリングダッシュボード & 対話型リアルタイム検索

Musubi には、VictoriaMetrics (TSDB) および PostgreSQL と連携した公式 Grafana ダッシュボードがプリセットされています。システムリソース、リアルタイム SNMP テレメトリ、MIB キャッシュ、シナリオ実行履歴、API 監査ログを一元監視できます。

* **ダッシュボード URL**: `http://localhost:3000/d/musubi-overview`

![Musubi Grafana Dashboard](docs/images/grafana_dashboard.png)

### 主な監視パネルと対話型フィルタリング機能
- **🎛️ ダッシュボード上部の対話型フィルター (Variables)**:
  - **`Target`**: ドロップダウンから対象機器（`All`, `spine1`, `spine2` 等）を即座に絞り込み
  - **`Trigger`**: 状態遷移の契機（`All`, `TRAP`, `INFORM`, `BULK_GET`, `POLLING`, `SET`, `API`）で抽出
  - **`Status`**: ジョブ実行ステータス（`All`, `SUCCESS`, `FAILED`, `RUNNING`, `QUEUED`, `ABORTED`）で選別
  - **`Search`**: フリーテキストによる部分一致検索（OID、MIB名、シナリオ名、実行者など）
  - **時間範囲セレクター**: 右上の Grafana 標準タイムピッカーと連動した期間指定検索（`$__timeFilter`）
- **🖥️ システムリソース & ネットワーク帯域**: CPU使用率、メモリ割り当て（Heap/Sys/RSS）、HTTP/SNMP帯域幅、Goroutine稼働数
- **🎯 ターゲット別 SNMP テレメトリ & MIB データキャッシュ**: IF-MIB, IP-MIB, SNMPv2-MIB, OSPF-MIB, BGP4-MIB などの最新値・前回値・取得元
- **⚡ シナリオ実行履歴 & 監査ログ (2000件+ ページネーション対応)**: 各テーブルパネルに 25件/ページのページネーションとインラインソート・検索を装備し、大量レコードでも軽快にブラウジング可能

---

## ⚡ 大規模MIB反映 & 高多重負荷ベンチマーク実測結果 (Benchmark Results)

Musubi の高性能・低遅延特性を検証するため、**Apple M2 (8 Cores: 4P+4E) / 16.0 GB RAM / macOS arm64** 環境にて実機負荷ベンチマークを実施しました。詳細は [負荷・性能ベンチマーク試験記録](docs/load_test_report.md) をご参照ください。

### 1. 2048 OID Bulk-Get / Walk ステータス反映試験
単一エージェントから 2048 個の MIB OID（256 インターフェース × 8 属性）を一括取得し、内部状態リポジトリ (`stateRepo`) へ反映する性能を測定。

| 測定項目 | 実測値 | 判定 |
| :--- | :--- | :--- |
| **対象 OID 総数** | **2048 OIDs** | - |
| **反映確認 OID 数** | **2048 / 2048 OIDs** | **100.0% 完全反映 (PASS)** |
| **処理所要時間 (P50)** | **24.09 ms** | 超高速 (P95: 25.16 ms / Avg: 24.34 ms) |
| **OID 取得スループット** | **84,146.38 OIDs/sec** | 秒間 8.4 万 OID 処理 |
| **CEL 式評価レイテンシ** | **90.35 µs / query** | サブミリ秒 (< 0.1ms) で即時判定 |
| **メモリ消費量 (Heap In-Use)** | **4.07 MB** | 16GB Spec に対し 0.03% 未満 |

### 2. 12 Agent 一斉並行シナリオ実行 (3,072 OIDs) 負荷試験
12 台の独立した SNMP エージェント（各 256 OIDs）に対して、12 本のシナリオジョブ（**Bulk-Get 256 OIDs → SNMP SET 障害注入 → Inform-Request ACK 待ち**）を一斉並行投入。

| 測定項目 | 実測値 | 判定・備考 |
| :--- | :--- | :--- |
| **同時並行 Agent 数** | **12 Agents** | 12 並列同時実行 |
| **同時処理 OID 総数** | **3,072 OIDs** | 256 OIDs × 12 Agents |
| **ジョブ成功率** | **60 / 60 (100.0%)** | **エラー 0 件 (PASS)** (5反復測定) |
| **一斉実行 総合所要時間** | **93.73 ms** | 全 12 ジョブ並列完了時間 |
| **ジョブレイテンシ (Min / P50 / P95 / Max)** | **5.17 ms / 10.55 ms / 41.11 ms / 41.17 ms** | 中央値 10.55ms の高速応答 |
| **ジョブ実行スループット** | **640.11 jobs/sec** | 高多重並行処理 |
| **OID 処理スループット** | **163,869.13 OIDs/sec** | 秒間 16 万 OID 処理 |
| **メモリ消費量 (Heap In-Use)** | **10.02 MB** | 16GB Spec に対し 0.06% 未満 |

```bash
# ローカル環境でのベンチマーク再実行コマンド
make benchmark
```

---

## 📚 ドキュメント一覧 (Documentation Index)

| ドキュメント | 対象読者 | 主な内容 |
| :--- | :--- | :--- |
| 📖 [ユーザー利用マニュアル](docs/user_manual.md) | 利用者・オペレーター | 実行方法、シナリオ作成詳細、CEL式リファレンス、運用手順、トラブルシューティング |
| 🏛️ [アーキテクチャ設計仕様書](docs/architecture.md) | 設計者・アーキテクト | モジュラーモノリス設計、ドメイン境界、シーケンス図、ライフサイクル置換表 |
| 🔧 [メンテナー向け保守ガイド](docs/maintenance_guide.md) | メンテナー・開発者 | 内部実装構造、カバレッジ基準 (80%+)、静的解析、デバッグ手順 |
| 🚀 [負荷・性能ベンチマーク試験記録](docs/load_test_report.md) | アーキテクト・評価者 | 2048 OID 反映試験、12 Agent × 256 OID 高多重シナリオ負荷試験実測データ (Apple M2) |
| 📄 [OpenAPI 3.1 仕様書](api/openapi.yaml) | API 連携開発者 | RESTful API エンドポイント、リクエスト/レスポンス JSON スキーマ定義 |

---

## ⚙️ 開発・検証コマンド一覧 (Development Commands)

| コマンド | 説明 |
| :--- | :--- |
| `make fmt` | ソースコードのフォーマット (`go fmt` / リンター自動修正) |
| `make lint` | `golangci-lint` による全パッケージの厳格な静的解析 |
| `make test` | データ競合検知 (`-race`) および 80% 基準カバレッジ測定付きテスト実行 |
| `make pcap-verify` | SNMP Bulk-Get / SET / Inform フローの自動実行と PCAP パケット構造解析 |
| `make benchmark` | 大規模 MIB 反映 (2048 OID) & 12 Agent 並列負荷ベンチマーク実行とレポート生成 |
| `make build` | `bin/` 配下への全実行可能バイナリ (`musubi-server`, `musubi-cli`, `mock-snmp-agent`) コンパイル |
| `make docker-test` | Docker Compose 環境での E2E 自動検証スクリプト実行 |
| `make openapi-lint` | Spectral による OpenAPI 3.1 スキーマ文法検証 |
| `make clean` | 一時ファイル、バイナリ成果物、テストキャッシュの削除 |

---

## 📄 ライセンス (License)

本プロジェクトは **Apache License 2.0** の下で公開されています。詳細については [LICENSE](LICENSE) ファイルをご参照ください。
