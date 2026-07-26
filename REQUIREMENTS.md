# SNMP Scenario Orchestrator プロジェクト要件仕様 (REQUIREMENTS.md)

本ドキュメントは **SNMP Scenario Orchestrator (Musubi)** のソフトウェア要件仕様（SRS）およびプロジェクトの要件達成状況を評価・管理するための要件一覧です。PDM (Product Development Manager) との議論および開発進捗の判定ベースとして利用します。

---

# 1. プロジェクト概要

SNMP Managerとして動作し、複数のSNMP Agentに対してシナリオベースで自動試験・状態監視・イベント駆動制御を実行するOSSネットワーク試験基盤を提供する。

### コア設計原則
1. **Event Driven**: Trap・Inform・Polling結果をイベントとして扱い、Stateの更新および条件判定（CEL）を通じて次のステップへ遷移する。
2. **State Driven**: Scenario Engine はプロトコル（SNMP）を直接参照せず、統合 State Store（`state.<target>.<key>`）を参照して条件評価を行う。
3. **Protocol Independent**: Scenario Engine はプロトコル非依存。SNMP 操作は独立した Action Plugin（`snmp.get`, `snmp.set`, `snmp.bulkget`）として実装する。

---

# 2. システムアーキテクチャ & コアコンポーネント

```
                              Grafana (Dashboard / Infinity Plugin)
                                       │
                                REST / WebSocket
                                       │
                                  API Server (Gin / OpenAPI 3.1)
                                       │
               ┌───────────────────────┴───────────────────────┐
               │                                               │
        Scenario Engine (DSL / CEL)                       Event Bus (Pub/Sub)
               │                                               │
         Action Engine                                   State Service
    ┌──────────┼──────────┐                                    │
snmp.get   snmp.set   sleep                              Memory / Valkey Store
    │          │                                               ▲
    └──────────┴────────────────┐                        │
                      SNMP Client                        Trap / Inform / Polling
                           │                                   │
                           └─────────────┐ ┌───────────────────┘
                                         ▼ ▼
                                  Target Network Devices / Mock Agents
```

---

# 3. コア機能要件・設計詳細

### 3.1. InformRequest & Trap ハンドリング
- **InformRequest 応答案 (ACK)**: PDU Type `gosnmp.InformRequest` 受信時、RequestID と Security パラメータを抽出し、**即座に `gosnmp.GetResponse` (ACK PDU) を送信元 IP:Port へ UDP 返信**。
- **Trap 受信**: UDP 162 ポート等で受信し、ACK 返信を行わずに State Store を更新および EventBus へイベント発行。

### 3.2. 拡張 MIB / ベンダー MIB 対応
- **動的 MIB 解析**: YAML/JSON 構造化スキーマまたは標準/拡張 MIB ファイルから OID <-> Name (例: `1.3.6.1.2.1.2.2.1.8.1` <-> `IF-MIB::ifOperStatus.1`) の双方向ルックアップテーブルを構築。
- **REST API 提供**: `/api/v1/mibs` 経由で MIB ツリー・OID 属性の検索・逆引き・動的インポートを提供。

### 3.3. ログ管理 & リテンションポリシー (Retention Policy)
- **`PollingLog`**: リテンション **7 日間** / 最大レコード数 **1,000,000 件**
- **`TrapLog` / `InformLog` / `JobStepLog`**: リテンション **30 日間** / 最大レコード数 **500,000 件**
- **`AuditLog`**: リテンション **365 日間** / 最大レコード数 **200,000 件**
- **自動クリーンアップ**: バックグラウンドジョブ (`internal/logcleaner`) が超過レコードおよび期限切れデータを自動 DELETE。

### 3.4. テスト戦略 & 純Go Mock SNMP Agent
- **純Go `gosnmp` Mock Agent (`internal/testutil/snmpmock`)**: `net-snmp` や Python `snmpsimd` への依存を廃止。
- **マルチ Agent 試験**: テスト実行時に複数の Mock Agent を動的 UDP ポートで起動し、並列シナリオ試験や Agent からの Trap/Inform 自発送信を検証。

### 3.5. 簡略化された State & Resume/Pause ライフサイクル
- **State Store**: メモリ（将来 Valkey）上で一括保持。
- **再起動時動作**: サービス再起動時は既存の未完シナリオを最初から実行（または Failed 終了）とし、複雑な再起動時ディープステート復元を廃止。

### 3.6. フロントエンド & Grafana 可視化
- **Grafana MIB Browsing**: **Grafana Infinity Data Source Plugin** 経由で Musubi の `/api/v1/mibs/tree` を参照し、MIB ツリーおよび OID 詳細を可視化。
- **Scenario Runner Web UI (内蔵 Web UI)**: API Server に `embed.FS` で組み込まれるシングルページ UI。事前登録されたシナリオの選択、パラメータ動的入力フォーム、実行指示、WebSocket によるリアルタイム進行状況追跡を提供。

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

### 3. SNMP Scenario Orchestrator 機能要件 (新機能要件)
- [ ] **R-3.1 Scenario Engine & DSL**: YAML (v1alpha1) の解析、`action`, `wait` (CEL式評価), `parallel` (Join対応), `loop`, `sleep` の制御ロジックが実装されていること。
- [ ] **R-3.2 State Service & Memory Store**: スレッドセーフな State Store (`state.<target>.<key>`) が実装され、状態更新時に EventBus へ自動通知されること。
- [ ] **R-3.3 Event Bus**: In-Memory Pub/Sub イベントバスが実装され、Trap, Inform, State Update, Timeout, Job Finish イベントを非同期配信できること。
- [ ] **R-3.4 Action Engine & SNMP Action Plugin**: `snmp.get`, `snmp.set`, `snmp.bulkget`, `sleep` プラグインが実装され、プロトコル非依存の抽象インターフェースで実行できること。
- [ ] **R-3.5 Trap & InformRequest Receiver (ACK対応)**: UDP 162 で Trap/Inform を受信し、`InformRequest` 受信時には即座に ACK レスポンス (`0xA2`) を返信するとともに State Store と EventBus を更新できること。
- [ ] **R-3.6 Polling Engine**: 指定されたターゲット・インターバルで SNMP GET / BULKGET を定期実行し、差分を検知して State を更新できること。
- [ ] **R-3.7 拡張 MIB 対応 & MIB API**: 標準・ベンダー拡張 MIB の解析・逆引きルックアップテーブルおよび `/api/v1/mibs` REST API が実装されていること。
- [ ] **R-3.8 ログ管理 & 自動クリーンアップ**: `PollingLog` (7日/100万件), `TrapLog`/`InformLog`/`JobStepLog` (30日/50万件), `AuditLog` (365日/20万件) の保持制限および自動削除ジョブが実装されていること。
- [ ] **R-3.9 純Go Mock SNMP Agent & マルチAgentテスト**: `gosnmp` を用いた純Go Mock SNMP Agent (`internal/testutil/snmpmock`) が実装され、複数 Agent の動的 UDP 起動と Trap/Inform 自発テストが自動化されていること。
- [ ] **R-3.10 内蔵 Scenario Runner Web UI**: `embed.FS` で配信される Web UI から事前登録シナリオを選択し、パラメータを動的入力して実行開始および WebSocket 進捗追跡ができること。
- [ ] **R-3.11 Grafana Infinity Plugin 連携**: `/api/v1/mibs/tree` API 経由で Grafana の Infinity Plugin から MIB 情報が正常にブラウズできること。
- [ ] **R-3.12 Secret & Security (RBAC/JWT)**: JWT 認証および RBAC (Viewer, Operator, Administrator) が Gin ミドルウェアで検証されていること。

---

## 📈 自己評価結果

- **合計要件数**: 21
- **達成要件数**: 11 / 23
- **適合率 (達成数/23)**: 47.83 %
