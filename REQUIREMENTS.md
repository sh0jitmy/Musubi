# SNMP Scenario Orchestrator プロジェクト要件仕様 (REQUIREMENTS.md)

本ドキュメントは **SNMP Scenario Orchestrator (Musubi)** のソフトウェア要件仕様（SRS）およびプロジェクトの要件適合度を評価・管理するための要件一覧です。PDM (Product Development Manager) との合意形成および開発進捗の判定ベースとして利用します。

---

# 1. プロジェクト概要

SNMP Managerとして動作し、複数のSNMP Agentに対してシナリオベースで自動試験・状態監視・イベント駆動制御を実行するOSSネットワーク試験基盤を提供する。

### コア設計原則
1. **Event Driven**: Trap・Inform・Polling結果をイベントとして扱い、Stateの更新および条件判定（CEL）を通じて次のステップへ遷移する。
2. **State Driven**: Scenario Engine はプロトコル（SNMP）を直接参照せず、統合 State Store（Raw State / Derived State）を参照して条件評価を行う。
3. **Protocol Independent & Extensible**: Scenario Engine はプロトコル非依存。SNMP 操作や外部連携は独立した Action Plugin（`snmp.get`, `snmp.set`, `snmp.bulkget`, `http.request`, `script.exec` 等）として実装し、DSL で表現困難な複雑処理もプラグインで対応可能とする。
4. **API-First & Headless**: ユーザー独自のフロントエンド、CI/CD、社内ポータルから完全制御可能な REST API & SSE (Server-Sent Events) を提供し、標準ダッシュボードとして Grafana & VictoriaMetrics を位置づける。
5. **Modular Monolith & Microservices-Ready**: 4つの独立ドメイン境界（Gateway, Orchestrator, State, Collector）による障害隔離と、単一バイナリ (ラップトップ) / 分散コンテナ (クラスタ) の両対応アーキテクチャ。

---

# 2. システムアーキテクチャ & コアコンポーネント

```
                            ┌─────────────────────────────────────────┐
                            │    User Frontend / Portal / CI/CD       │
                            │    & Standard Grafana Dashboards        │
                            │    & Musubi CLI (urfave/cli)            │
                            └────────────────────┬────────────────────┘
                                                 │ REST / SSE (OpenAPI 3.1)
┌────────────────────────────────────────────────▼─────────────────────────────────────────┐
│ API Server (Gin)                                                                          │
│  ├─ Scenario Runner & Management API (/api/v1/scenarios, /api/v1/jobs)                   │
│  ├─ State & Transition Observability API (/api/v1/states/raw, /derived, /transitions)    │
│  ├─ Event Stream API (SSE: /api/v1/events/stream, LongPoll: /api/v1/events/poll)         │
│  ├─ Backup & GitOps Export/Import API (/api/v1/scenarios/export, /import)                │
│  └─ MIB Browser API (/api/v1/mibs)                                                       │
└───────────────────────┬─────────────────────────────────────────┬────────────────────────┘
                        │                                         │
        ┌───────────────▼───────────────┐         ┌───────────────▼───────────────┐
        │ Scenario Engine (DSL / CEL)   │◄────────┤ NotificationHub (Pub/Sub)     │
        └───────────────┬───────────────┘ (Notice)└───────────────▲───────────────┘
                        │                                         │ (StateChangedNotice)
                 Action Engine                             State Service
       ┌────────────────┼────────────────┐         (Diff Detection & State Mgmt)
   snmp.get         snmp.set       CustomPlugins                  │
   snmp.bulkget     sleep         (script, http)   ┌──────────────┴──────────────┐
       │                │                │         │ StateStore                  │
       └────────────────┴────────────────┘         │  ├─ Raw State (SNMP Data)   │
                                │                  │  └─ Derived State (Context) │
                                ▼                  └──────────────▲──────────────┘
                       Target Network Devices                     │
                     & Pure-Go Mock Agents ──────── Trap / Inform / Polling
```

---

# 3. コア機能要件・設計詳細

### 3.1. NotificationHub & 責務分離
- **`NotificationHub`**: 外部メッセージバスとの混同を防ぐため改名。責務を「通知のIn-Memory Pub/Sub配送」に限定し、状態や差分検知ロジックは一切持たない。
- **`StateService`**: 状態管理の単一責任を持ち、新旧データの差分検知（State Transition）および `StateChangedNotice` の発行、状態変化履歴の記録を担当。

### 3.2. StateStore の 2 層分離（Raw State & Derived State）
- **Raw State**: SNMP GET / BULKGET / Trap / Inform 等のプロトコル観測生データを保持（`raw.<target>.<oid_or_name>`）。
- **Derived State**: シナリオ実行コンテキスト、計算値、ステップ変数、集約判定フラグを保持（`derived.<namespace>.<key>`）。

### 3.3. Action Plugin 拡張性（DSL限界対応）
- YAML DSL では表現が困難な高度な外部連携・計算・スクリプト実行等に対応するため、`ActionPlugin` インターフェースによるカスタムプラグイン追加機構を提供。

### 3.4. 状態・イベント・変化履歴の可観測性 API
- `GET /api/v1/states/raw`: 現在の Raw State 一覧取得。
- `GET /api/v1/states/derived`: 現在の Derived State 一覧取得。
- `GET /api/v1/states/transitions`: 状態変化履歴（Transition Log）一覧取得。
- `GET /api/v1/events` & `/ws`: イベント/通知履歴の取得および WebSocket リアルタイム配信。

### 3.5. InformRequest & Trap ハンドリング
- **InformRequest 応答案 (ACK)**: PDU Type `gosnmp.InformRequest` 受信時、RequestID と Security パラメータを抽出し、**即座に `gosnmp.GetResponse` (ACK PDU) を送信元 IP:Port へ UDP 返信**。
- **Trap 受信**: UDP 162 ポート等で受信し、ACK 返信を行わずに State Store を更新および NotificationHub へ通知発行。

### 3.6. 拡張 MIB / ベンダー MIB 対応
- **動的 MIB 解析**: YAML/JSON 構造化スキーマまたは標準/拡張 MIB ファイルから OID <-> Name の双方向ルックアップテーブルを構築。
- **REST API 提供**: `/api/v1/mibs` 経由で MIB ツリー・OID 属性の検索・逆引き・動的インポートを提供。

### 3.7. ログ管理 & リテンションポリシー・性能目標 (Retention & High-Scale Budget)
- **確定トラフィック仕様**: 128 sets @ 1s/1OID (128 GET/s), 10min/4MIB BulkGet (Jitter分散), 定常 avg 96 Traps/s, バースト 512 Traps/s, 48 並行シナリオ実行。
- **Change-Only 記録 & バッチ書き込み**: 定常ポーリング (128 GET/s) はメモリ管理とし、差分・エラー時のみバッチ INSERT することでラップトップのディスク負荷を極小化 (IOPS 100〜300)。
- **デフォルト設計基準値**:
  - `job_step_logs`: 30 日間 / 300,000 件 (~0.20 GB)
  - `audit_logs`: 360 日間 (API全要求・操作含む) / 500,000 件 (~0.30 GB)
  - `polling_logs`: 30 日間 (Change-Only) / 1,000,000 件 (~0.45 GB)
  - `debug_logs`: 5 日間 / 300,000 件 (~0.15 GB)
  - `state_transition_logs` / `trap_logs` / `inform_logs`: 90 日間 / 各 500,000〜1,000,000 件 (~1.30 GB)
- **推奨ストレージ**: **5 GB 〜 7 GB** (SSD / NVMe / Docker Volume, 純データ容量 ~2.45 GB)
- **性能目標値 (ラップトップ基準)**: CPU 定常時 < 10% / 48並行試験時 < 30% / バースト時 < 48% (1コア換算 / 8コア全体で 1〜6%)、RAM システム全体 < 850 MB (Musubi 単体 < 350 MB)。
- **ユーザー設定可能**: 日数・件数上限・フラッシュ間隔は `config.yaml` で完全カスタマイズ可能。
- **自動クリーンアップ**: バックグラウンドジョブ (`internal/logcleaner`) が日次パージ。

### 3.8. テスト戦略 & 純Go Mock SNMP Agent
- **純Go `gosnmp` Mock Agent (`internal/testutil/snmpmock`)**: `net-snmp` や Python `snmpsimd` への依存を廃止。
- **マルチ Agent 試験**: テスト実行時に複数の Mock Agent を動的 UDP ポートで起動し、並列シナリオ試験や Agent からの Trap/Inform 自発送信を自動テスト。

### 3.9. フロントエンド & API / CLI 戦略 (SSE & CLI-First)
- **API-First**: ユーザー独自フロントエンドや CI/CD からの完全制御を前提とした OpenAPI 3.1 仕様。
- **SSE & Long-Polling リアルタイム配信**: プロキシ環境で制限のある WebSocket を廃止し、**Server-Sent Events (SSE - `text/event-stream`)** および **Long Polling** を標準採用。
- **公式 CLI ツール (`musubi-cli`)**: `github.com/urfave/cli/v2` を用いたターミナル用クライアントを提供（シナリオ実行・SSEリアルタイム進捗追跡・状態ウォッチ・MIB検索・バックアップ操作）。
- **認可 (RBAC) 基盤**: `github.com/casbin/casbin/v2` によるロールベースアクセス制御（Viewer, Operator, Administrator）をローカル自己完結で提供。
- **VictoriaMetrics 連携**: 各サービス別（Gateway, Orchestrator, State, Collector）の性能メトリクスを VictoriaMetrics (OSS) で収集し Grafana で可視化。
- **リファレンス Web UI**: シナリオ選択、動的パラメータ入力フォーム、実行、SSE リアルタイム進捗追跡画面を内蔵 (`//go:embed`)。

### 3.10. 運用保守 & バックアップ・リストア設計 (Operations, Backup & Restore)
- **フルシステムバックアップ/リストア**: `musubi-cli backup create` / `restore apply` による PostgreSQL データ、設定、カスタム MIB の一括保管とトランザクション復元。
- **GitOps シナリオ一括エクスポート/インポート**: `/api/v1/scenarios/export` & `/import` による環境間シナリオ YAML 移行。
- **Liveness & Readiness ヘルスチェック**: `/healthz` & `/readyz` による自己診断。

---

# 4. 要件チェックリスト

## 📋 要件チェックリスト

### 1. プロジェクト初期セットアップ & CI/CD
- [x] **R-1.1 モジュール名のカスタマイズ**: `go.mod` 内のモジュール名を自身のGitHubリポジトリ名に変更し、サポートするGoのバージョン（最新Go 1.26およびその直前1.25以上）が正しく指定されていること。
- [x] **R-1.2 GitHub Actions 権限設定**: `tagpr` や `goreleaser` が動作するように、GitHubリポジトリの `Settings > Actions > General > Workflow permissions` で 「Read and write permissions」が許可されていること。

### 2. 品質・セキュリティ基盤
- [x] **R-2.1 セキュアロギング**: `main.go` にて `slog` のマスキング処理が実装されており、ログへの機密情報漏洩が防止されていること。
- [x] **R-2.2 静的解析のクリア**: `make lint` を実行し、すべての静的解析警告がないクリーンな状態であること。
- [x] **R-2.3 脆弱性診断のクリア**: `make vulncheck` を実行し、パッケージ脆弱性がないこと。
- [x] **R-2.4 テストとビルドの保証**: `make test` および `make build` が正常にパスすること。
- [x] **R-2.5 自動リリースの統合**: `.tagpr` および `.goreleaser.yaml`、各種GitHub Actionsワークフローが配置され、リリースフローが定義されていること。
- [x] **R-2.6 単体テスト(UT)の網羅**: 実装されたすべての関数・メソッドおよび主要ロジックに対して、対応する単体テスト（UT）が作成されパスしていること。
- [x] **R-2.7 テスト品質の自動検証**: `TestMain` での goroutine リーク検出（`goleak`）および `golangci-lint` でのテスト品質監査が設定されていること。
- [x] **R-2.8 ライセンス＆作成者ヘッダーの自動監査**: `make license-check` を通して Go ソースファイルのライセンス・作成者ヘッダーの欠落を自動検証できること。
- [x] **R-2.9 OpenAPI 駆動開発の準拠**: `api/openapi.yaml` を定義し、REST API の仕様が公開されること。

### 3. 要件定義・アーキテクチャ設計仕様 (PDM合意済み)
- [x] **R-3.1 Scenario Engine & DSL 仕様策定**: YAML (v1alpha1) の解析、`action`, `wait` (CEL式評価), `parallel` (Join対応), `loop`, `sleep` の制御ロジック仕様が策定・合意されていること。
- [x] **R-3.2 State Service & 2層StateStore (Raw/Derived) 設計**: Raw State（SNMP観測値）と Derived State（シナリオコンテキスト）の分離管理、差分検知（State Transition）および変化履歴記録仕様が策定・合意されていること。
- [x] **R-3.3 NotificationHub 責務設計**: In-Memory Pub/Sub による通知専用機構として設計され、状態管理ロジックと明確に分離されていること。
- [x] **R-3.4 Action Engine & 拡張Plugin基盤設計**: `snmp.get`, `snmp.set`, `snmp.bulkget`, `sleep` に加え、DSL で対応困難な処理を拡張可能な Action Plugin 仕様が策定されていること。
- [x] **R-3.5 状態・イベント・変化履歴 API 仕様策定**: `/api/v1/states/raw`, `/derived`, `/transitions`, `/events/stream` の OpenAPI 3.1 仕様が策定されていること。
- [x] **R-3.6 Trap & InformRequest (ACK対応) 受信設計**: UDP 162 での受信および `InformRequest` に対する即時 ACK レスポンス (`0xA2`) 返信シーケンスが設計されていること。
- [x] **R-3.7 Polling Engine 設計**: ターゲット・インターバルごとの SNMP GET / BULKGET 定期実行と差分検知仕様が設計されていること。
- [x] **R-3.8 拡張 MIB 対応 & MIB API 設計**: 標準・ベンダー拡張 MIB の解析・逆引きルックアップテーブルおよび `/api/v1/mibs` REST API 仕様が策定されていること。
- [x] **R-3.9 ログ管理・リテンション & 性能設計**: job_step(30日), audit(360日), debug(5日), polling(30日), 他(90日) の設計基準値 (ストレージ 5〜7GB), トラフィック仕様 (128 sets @ 1s/1OID, 10min/4MIB BulkGet Jitter分散, 定常 avg 96 Traps/s, バースト 512 Traps/s, 48 並行シナリオ, CPU < 30%, RAM < 850MB, IOPS 100〜300), `config.yaml` ユーザー設定可能性および自動削除ジョブ仕様が策定されていること。
- [x] **R-3.10 純Go Mock SNMP Agent & マルチAgentテスト戦略**: `gosnmp` を用いた純Go Mock SNMP Agent (`internal/testutil/snmpmock`) と複数 Agent 並列テスト戦略が策定されていること。
- [x] **R-3.11 ユーザー独自フロントエンド対応 API & 内蔵 Scenario Runner Web UI 設計**: 外部フロントエンドや CI/CD から完全操作可能な REST API と、内蔵パラメーター入力・SSE リアルタイム進捗追跡 Web UI 仕様が策定されていること。
- [x] **R-3.12 Grafana & VictoriaMetrics 連携設計**: VictoriaMetrics による各サービス性能メトリクス収集、標準ダッシュボードでのメトリクス表示および Infinity Plugin による MIB ブラウズ連携仕様が策定されていること。
- [x] **R-3.13 Secret & Security (Casbin RBAC/JWT) 設計**: JWT 認証および `github.com/casbin/casbin/v2` による RBAC (Viewer, Operator, Administrator) のローカル認可セキュリティモデルが設計されていること。
- [x] **R-3.14 モジュラーモノリス ＆ マイクロサービス分離設計**: 4つの独立ドメイン境界 (Gateway, Orchestrator, State, Collector) による障害隔離、単一バイナリ (ラップトップ) と分散コンテナ (クラスタ) の両対応アーキテクチャが策定されていること。
- [x] **R-3.15 完全 OSS ＆ エアギャップ（閉域網）運用保証**: クラウド・有償SaaS依存ゼロ、外部CDN依存ゼロ (Web UI 内蔵 //go:embed)、全コンポーネントが自由なOSSライセンス (MIT, Apache 2.0, BSD, PostgreSQL License) で構成され、完全オフラインで動作可能であること。
- [x] **R-3.16 公式 CLI ツール (`musubi-cli`) 設計**: `github.com/urfave/cli/v2` を採用し、シナリオ実行・SSEリアルタイム進捗追跡・状態ウォッチ・バックアップをターミナル/CI-CDから操作可能な CLI 仕様が策定されていること。
- [x] **R-3.17 運用保守・バックアップ＆リストア設計**: `musubi-cli backup create/restore apply` による PostgreSQL・設定・MIB の一括バックアップ/復元および GitOps シナリオ一括エクスポート/インポート仕様が策定されていること。
- [x] **R-3.18 SSE & Long-Polling リアルタイム通信設計**: プロキシ制約のある WebSocket を排し、標準 HTTP/1.1 ベースの Server-Sent Events (`/api/v1/events/stream`) および Long-Polling (`/api/v1/events/poll`) によるイベント配信仕様が策定されていること。
- [x] **R-3.19 Ent ORM & Clean DDD レイヤードアーキテクチャ設計**: 全データモデルを `entgo.io/ent` で型安全に一元管理し、各 Bounded Context（Collector, State, Orchestrator, Gateway）が Domain / Application / Infrastructure の 3 層構造および Shared Kernel (`internal/common`) による共通基盤集約（エラー, バッチ処理, テレメトリ）として設計されていること。
- [x] **R-3.20 フロントエンド E2E 自動テスト設計**: ヘッドレスブラウザによる Web UI ロード、動的パラメータフォーム生成、SSE リアルタイム進捗 DOM 更新、MIB/State 表示の完全オフライン E2E 自動検証仕様が策定されていること。
- [x] **R-3.21 エアギャップ向けラップトップ単一アーカイブ展開設計**: インストーラー不要・事前ダウンロード前提の単一アーカイブ (`musubi-bundle.tar.gz`)、事前保存コンテナイメージ (`docker-images.tar.gz`)、ワンタッチ起動スクリプト (`start.sh`) による 2 ステップ完全オフライン起動仕様が策定されていること。
- [x] **R-3.22 証跡提出・監査ログ抽出設計**: `musubi-cli audit export` および REST API により、期間内の全操作ログ・シナリオ実行証跡・ハッシュ署名をまとめた一括監査提出パッケージ (`zip`) の出力仕様が策定されていること。
- [x] **R-3.23 状態変更履歴・差分ログ吸い出し設計**: `musubi-cli state export-transitions` および REST API により、ネットワーク機器パラメータの Before/After 差分履歴タイムライン（CSV/JSON）の抽出仕様が策定されていること。
- [x] **R-3.24 ターゲット & リソース定義・ライフサイクル管理設計**: SNMP v1 / v2c / v3 (AuthPriv) に完全対応した Target & CredentialProfile データモデル、疎通テスト (Ping)、一括 YAML インベントリ (`targets.yaml`) プロビジョニング、および並行シナリオ実行時のターゲット排他リースロック仕様が策定されていること。
- [x] **R-3.25 Web UI ターゲット管理 & 作成・更新・削除 変更履歴（Audit Trail）設計**: 内蔵 Web UI によるターゲット・認証プロファイルの視覚的一覧・登録・編集・削除・Ping テスト機能、および「誰が・いつ・何を・どのように（Before/After 差分）」変更したかを記録・追跡する完全な変更履歴 (Audit Trail) API & UI 仕様が策定されていること。
- [x] **R-3.26 ターゲット ⇔ シナリオ間の相互依存・整合性制御設計**: 実行中ジョブ保護 (409 Conflict)、静的参照シナリオ保護 (400 Bad Request)、論理削除 (Soft Delete)、シナリオ登録時警告付き許可、およびジョブ実行前の厳格なターゲット事前検査 (Pre-flight Hard Block - 422) による整合性保護仕様が策定されていること。
- [x] **R-3.27 孤立・不要シナリオの一括バッチクリーンアップ設計**: 削除済みターゲットに依存する孤立シナリオの自動検出、削除前自動バックアップ付き一括パージ (`musubi-cli scenario cleanup` & REST API)、ターゲット削除時の連動クリーンアップ (`--cleanup-scenarios`)、および Web UI 整理機能仕様が策定されていること。
- [x] **R-3.28 ライフサイクル組み合わせ・順序検証マトリクス (C-01〜C-14) 策定**: Target追加/更新/削除 × シナリオ登録/更新/削除 × 実行中排他/保守モード等の全14パターンの組み合わせ・実行順序マトリクスが策定され、UT/E2E自動テストシナリオへ反映されていること。
- [x] **R-3.29 ユーザー体験 (UX) ＆ インタラクション設計**: SSE リアルタイム状況透明性 (wait.until 待機理由可視化)、YAML 定義からのスマート入力フォーム自動生成、行動可能なエラー案内、状態変化フラッシュアニメーション＆Git風Diff表示、および TUI カラー進捗を含む 5 大 UX ガイドライン仕様が策定されていること。
- [x] **R-3.30 削除飢餓（Starvation）防止 ＆ 段階的排水（Drain）/ 強制中断（Force-Abort）削除設計**: 外部からの連続シナリオ実行要求時でもターゲットやシナリオを安全に削除できる段階的排水モード (`status: DRAINING`)、および緊急用即時強制中断・削除 (`--force-abort`) 仕様が策定されていること。
- [x] **R-3.31 REST API 完全設計 ＆ RFC 9457 エラー契約 (Idempotency, Actionable Guidance)**: ターゲット/シナリオ/ジョブ/状態/監査/運用の全REST API完全仕様、多重実行防止 (`Idempotency-Key`)、および解決サジェスト・誘導リンクを含む RFC 9457 Problem Details 統一エラー契約仕様が策定されていること。
- [x] **R-3.32 Docker Compose 一括環境 (Grafana, Postgres, VictoriaMetrics, Mock SNMP) ＆ デモ/E2E 自動検証**: 本番・導入デモ用の Docker Compose 一括スタック、事前プロビジョニング済み Grafana ダッシュボード、PostgreSQL DB、VictoriaMetrics TSDB、SNMP モックエージェント、およびワンコマンド実行可能なデモスクリプト (`demo.sh`) と Docker E2E 自動検証スイート (`docker_e2e.sh`) が完備されていること。
- [x] **R-3.33 Grafana UI レンダリング・メトリクス値検証 ＆ HTML レポート自動出力 (E2E)**: ヘッドレスブラウザによる Grafana ダッシュボードの常時表示検証、全パネルのメトリクス値（Goroutines, Memory, Uptime, Targets, Jobs）の自動アサーション、高解像度スクリーンショットの埋め込み、および単一自己完結型 HTML テストレポート (`test_reports/grafana_e2e_report.html`) の自動生成が完備されていること。

---

## 📈 自己評価結果

- **合計要件数**: 42
- **達成要件数**: 44 / 44
- **適合率 (達成数/44)**: 100.00 %














