# Musubi ユーザー利用マニュアル (User Manual & Operations Guide)

**SNMP Scenario Orchestrator (Musubi)** は、複数の SNMP Agent（ネットワーク機器）に対してシナリオベースで自動試験・状態監視・イベント駆動制御を実行する OSS ネットワーク試験・運用自動化基盤です。

本ドキュメントは、Musubi を初めて導入するエンジニアから、日常的にシナリオ作成・実行・システム運用を行うオペレーターまでを対象とした総合利用マニュアルです。

---

## 📑 目次

1. [Musubi の概要と基本コンセプト](#1-musubi-の概要と基本コンセプト)
2. [クイックスタート & 環境構築](#2-クイックスタート--環境構築)
3. [認証プロファイルとターゲット機器の管理](#3-認証プロファイルとターゲット機器の管理)
4. [シナリオ作成パーフェクトガイド (DSL & CEL 仕様)](#4-シナリオ作成パーフェクトガイド-dsl--cel-仕様)
5. [シナリオの登録・実行・進捗監視](#5-シナリオの登録実行進捗監視)
6. [Grafana ダッシュボードによるリアルタイム監視](#6-grafana-ダッシュボードによるリアルタイム監視)
7. [運用・保守・ライフサイクル管理](#7-運用保守ライフサイクル管理)
8. [トラブルシューティング & FAQ](#8-トラブルシューティング--faq)
9. [ユーザー体験 (UX/DX) 向上のためのベストプラクティス](#9-ユーザー体験-uxdx-向上のためのベストプラクティス)

---

## 1. Musubi の概要と基本コンセプト

### 1.1 Musubi が解決する課題
* **エアギャップ環境での自律試験**: インターネット接続のないセキュアな検証ラボ環境でも、外部依存ゼロ（単一バイナリ / Docker コンテナ）で完結動作します。
* **Trap/Inform 連動のリアルタイム検証**: 定期ポーリング待ちのタイムラグを排除し、機器から送信される SNMP Trap / Inform をミリ秒単位で検知してシナリオを即座に進捗させます。
* **CEL (Common Expression Language) による宣言的状態評価**: 複雑なスクリプトを書くことなく、`raw['spine1']['IF-MIB::ifOperStatus.1'] == 'up'` のような宣言的な式で合否判定が可能です。
* **排他ロックとライフサイクル保護**: 複数エンジニアや並行テストによるターゲット機器の競合を Lease Lock（リースロック）で防ぎ、設定変更中の機器破壊や誤操作を未然に防止します。

### 1.2 主要コンセプト用語集

```mermaid
graph LR
    Credential[CredentialProfile\nSNMP v2c/v3認証情報] --> Target[Target Device\nネットワーク機器]
    Target --> LeaseLock[Target Lease Lock\n実行中排他制御]
    Scenario[Scenario DSL\nYAML定義ファイル] --> Job[Job Execution\nシナリオ実行インスタンス]
    Job --> LeaseLock
    Target --> SNMPCollector[SNMP Collector\nTrap/Inform/Polling]
    SNMPCollector --> StateRepo[State Repository\nRaw / Derived State]
    StateRepo --> CELEvaluator[CEL Evaluator\nwait.until 条件判定]
    CELEvaluator --> Job
```

| 用語 | 説明 |
| :--- | :--- |
| **Target (ターゲット)** | 試験・監視対象となるネットワーク機器（ルータ、スイッチ、ファイアウォール、サーバー等）。ホスト名/IP、ポート、状態を保持します。 |
| **CredentialProfile (認証プロファイル)** | SNMP v1, v2c (Community), v3 (USM: MD5/SHA/SHA256, DES/AES) の接続クレデンシャル定義。複数のターゲットで共有可能。 |
| **Scenario DSL (シナリオDSL)** | 試験手順を YAML で記述した定義体。実行に必要なターゲットロック、入力パラメータ、ステップ手順、Teardown を定義します。 |
| **Job (ジョブ)** | シナリオを実行した際の実行単位（インスタンス）。実行ステータス（`QUEUED`, `RUNNING`, `SUCCESS`, `FAILED`, `ABORTED`）や各ステップの結果を記録します。 |
| **State Repository (状態リポジトリ)** | 機器から収集された最新の SNMP 状態（`Raw State`）や、複数状態を集約した派生状態（`Derived State`）をメモリ上でインデックス管理するストア。 |
| **CEL (Common Expression Language)** | Google が開発した高速・安全な式評価言語。シナリオの `wait.until` 条件判定に採用されています。 |
| **Notification Hub** | 状態変化やジョブ進捗イベントを SSE (Server-Sent Events) や Long-Polling でリアルタイム配信するPubSubエンジン。 |

---

## 2. クイックスタート & 環境構築

### 2.1 前提条件
* **Docker & Docker Compose** (推奨環境: Docker 24.0+, Docker Compose v2.20+)
* または **Go 1.22+** (ローカルバイナリとしてビルド・実行する場合)

---

### 2.2 Docker Compose による即時起動 (推奨)

リポジトリルートで以下のコマンドを実行するだけで、Musubi Server、Mock SNMP Agent、VictoriaMetrics (TSDB)、Grafana が自動構成されて起動します。

```bash
# コンテナのビルドおよびバックグラウンド起動
docker compose up -d --build
```

#### 起動サービスのポート一覧

| サービス名 | ポート | プロトコル | 用途 |
| :--- | :--- | :--- | :--- |
| **musubi-server** | `8080` | HTTP | Musubi REST API, SSE ストリーム, `/metrics` エンドポイント |
| **musubi-server** | `162` | UDP | SNMP Trap / Inform 受信リスナー (ポートマッピング: `162:162/udp`) |
| **mock-snmp-agent** | `161` | UDP | 純Go 実装のテスト用 SNMP Mock エージェント |
| **victoriametrics** | `8428` | HTTP | 高性能 TSDB（Prometheus 互換メトリクス収集・保存） |
| **grafana** | `3000` | HTTP | リアルタイム可視化ダッシュボード (`http://localhost:3000`) |
| **postgres** | `5432` | TCP | リレーショナル永続化ストレージ (`musubi/musubi_secret`) |

```bash
# 健全性確認 (Health Check)
curl -s http://localhost:8080/v1/system/healths | jq .
```

---

### 2.3 ローカルバイナリでのビルド & 起動

コンテナを使わず、開発端末やオンプレミスサーバーのホストOS上で直接起動する場合の手順です。

```bash
# 1. 全バイナリの一括コンパイル (bin/ に生成されます)
make build

# 2. Musubi サーバーの起動 (デフォルト: SQLite インメモリ / ローカルDB)
./bin/musubi-server

# 別ターミナルで Mock SNMP Agent を起動 (必要に応じて)
./bin/mock-snmp-agent
```

---

## 3. 認証プロファイルとターゲット機器の管理

### 3.1 認証プロファイル (CredentialProfile) の作成

ターゲット機器へ接続するための SNMP 認証情報を登録します。

#### CLI による作成
```bash
# SNMP v3 (AuthPriv) 認証プロファイルの作成
./bin/musubi-cli credentials create \
  --name "v3-core-admin" \
  --version "v3" \
  --sec-level "authPriv" \
  --username "snmpadmin" \
  --auth-proto "SHA256" \
  --auth-pass "AuthPassword123!" \
  --priv-proto "AES" \
  --priv-pass "PrivPassword123!"

# SNMP v2c (Community) 認証プロファイルの作成
./bin/musubi-cli credentials create \
  --name "v2c-public" \
  --version "v2c" \
  --community "public"
```

#### REST API による作成
```bash
curl -X POST http://localhost:8080/v1/credentials \
  -H "Content-Type: application/json" \
  -d '{
    "name": "v3-core-admin",
    "version": "v3",
    "sec_level": "authPriv",
    "username": "snmpadmin",
    "auth_protocol": "SHA256",
    "auth_passphrase": "AuthPassword123!",
    "priv_protocol": "AES",
    "priv_passphrase": "PrivPassword123!"
  }'
```

---

### 3.2 ターゲット機器 (Target) の登録

```bash
# CLI によるターゲット登録
./bin/musubi-cli targets create \
  --name "spine1" \
  --host "192.168.10.1" \
  --port 161 \
  --credential "v3-core-admin" \
  --labels "role=spine,site=dc1,model=switch-x"

# 疎通確認 (SNMP Ping)
./bin/musubi-cli targets ping --name "spine1"
```

#### REST API によるターゲット登録
```bash
curl -X POST http://localhost:8080/v1/targets \
  -H "Content-Type: application/json" \
  -d '{
    "name": "spine1",
    "description": "DC1 Core Spine Switch 1",
    "host": "192.168.10.1",
    "port": 161,
    "credential_id": "cred-v3-core-admin",
    "labels": {
      "role": "spine",
      "site": "dc1"
    }
  }'
```

---

### 3.3 ターゲット機器のステータスとドレイン (Drain)

ターゲット機器には以下のステータスが存在します：

```
 ONLINE ──(Drain API)──> DRAINING ──(全Job完了)──> OFFLINE / DELETED
    │
    └──(Maintenance API)──> MAINTENANCE (試験停止・ロック拒否)
```

1. **ONLINE**: 通常稼働状態。新規ジョブのロック獲得およびシナリオ実行が可能。
2. **DRAINING (ドレイン)**: 計画メンテナンス用。**実行中のジョブ完了を待ちつつ、新規ジョブのロック獲得をブロック**します。
   ```bash
   # CLI でドレインを開始
   ./bin/musubi-cli targets drain --name "spine1"
   ```
3. **MAINTENANCE**: メンテナンス中。事前検証 (Pre-flight) で直ちに `422 Unprocessable Entity` として拒否されます。
4. **OFFLINE / DELETED**: 停止または削除済み。

---

## 4. シナリオ作成パーフェクトガイド (DSL & CEL 仕様)

Musubi では、試験手順をシンプルかつ可読性の高い **YAML DSL** で記述します。

### 4.1 シナリオ YAML の基本構造

```yaml
# ==============================================================================
# シナリオ基本情報
# ==============================================================================
name: "spine-linkdown-failover-test"
description: "Spine1のインターフェース切断によるBGPフェイルオーバー検証"
version: 1

# ==============================================================================
# ターゲット排他ロック (必須)
# シナリオ開始時に排他リースロックを獲得し、終了時に自動解放します。
# ==============================================================================
target_locks:
  - "spine1"
  - "spine2"

# ==============================================================================
# 動的入力パラメータ定義 (CLIやAPIから実行時に上書き可能)
# ==============================================================================
inputs:
  target_interface:
    type: "string"
    description: "試験対象のインターフェースOIDインデックス"
    default: "1"
    required: true
  wait_timeout:
    type: "string"
    description: "状態収束待ちの最大タイムアウト時間"
    default: "10s"

# ==============================================================================
# メイン試験ステップ (上から順に実行)
# ==============================================================================
steps:
  # ステップ1: 初期状態の確認 (リンクが UP であること)
  - id: "step1_verify_initial_link_up"
    name: "初期リンク稼働状態の確認"
    target: "spine1"
    wait:
      until: "raw['spine1']['IF-MIB::ifOperStatus.' + inputs.target_interface] == 'up' || raw['spine1']['1.3.6.1.2.1.2.2.1.8.' + inputs.target_interface] == 1"
      timeout: "${inputs.wait_timeout}"
      interval: "200ms"

  # ステップ2: SNMP SET による意図的なリンクダウン発生 (障害注入)
  - id: "step2_inject_link_down"
    name: "Spine1 インターフェースの意図的ダウン (ifAdminStatus=down)"
    target: "spine1"
    action: "action.snmp_set"
    params:
      oid: "1.3.6.1.2.1.2.2.1.7.${inputs.target_interface}"  # IF-MIB::ifAdminStatus
      type: "int"
      value: 2                                               # 2 = down
    ignore_error: false

  # ステップ3: Trap または Polling によるリンクダウンの検知待ち
  - id: "step3_wait_for_oper_status_down"
    name: "リンクダウン検知の確認"
    target: "spine1"
    wait:
      until: "raw['spine1']['IF-MIB::ifOperStatus.' + inputs.target_interface] == 'down' || raw['spine1']['1.3.6.1.2.1.2.2.1.8.' + inputs.target_interface] == 2"
      timeout: "5s"
      interval: "100ms"

  # ステップ4: 対向機 (Spine2) への経路切り替え確認
  - id: "step4_verify_failover_route"
    name: "Spine2 のアクティブ経路昇格確認"
    target: "spine2"
    wait:
      until: "derived['cluster.primary_spine'] == 'spine2'"
      timeout: "10s"
      interval: "500ms"

# ==============================================================================
# クリーンアップ / ロールバック手順 (Teardown)
# stepsが途中で失敗またはタイムアウトした場合でも、必ず最後に実行されます。
# ==============================================================================
teardown:
  - id: "teardown_restore_link"
    name: "Spine1 インターフェースの復旧 (ifAdminStatus=up)"
    target: "spine1"
    action: "action.snmp_set"
    params:
      oid: "1.3.6.1.2.1.2.2.1.7.${inputs.target_interface}"
      type: "int"
      value: 1                                               # 1 = up
    ignore_error: true
```

---

### 4.2 CEL (Common Expression Language) 式リファレンス

`wait.until` 条件式では、高速かつ型安全な Google CEL 式を使用します。

#### 使用可能な組み込みスコープ

| スコープ | 説明 | 記法例 |
| :--- | :--- | :--- |
| `raw['<target>']['<oid_or_name>']` | 指定ターゲットの最新 SNMP 取得値（Trap/Inform/Get で随時更新） | `raw['spine1']['IF-MIB::ifOperStatus.1'] == 'up'` |
| `derived['<key>']` | システム内で集約・計算された派生ステータス | `derived['cluster.health'] == 'HEALTHY'` |
| `inputs['<key>']` または `inputs.<key>` | シナリオ実行時に渡された動的入力値 | `raw['spine1']['status'] == inputs.expected_val` |

#### よく使う CEL 演算子・関数一覧

```javascript
// 1. 等値・比較演算
raw['spine1']['status'] == 1
raw['spine1']['cpu_usage'] < 80.0
raw['spine1']['bgp_peer_count'] >= inputs.min_peers

// 2. 論理演算 (AND / OR / NOT)
(raw['spine1']['link1'] == 'up') && (raw['spine2']['link1'] == 'up')
raw['spine1']['oper'] == 'down' || raw['spine1']['admin'] == 'down'
! (raw['spine1']['is_rebooting'] == true)

// 3. 文字列操作・正規表現
raw['spine1']['sysDescr'].contains("RouterOS")
raw['spine1']['hostname'].startsWith("tokyo-core-")
raw['spine1']['version'].matches("^v[0-9]+\\.[0-9]+")

// 4. コレクション・リスト判定
raw['spine1']['active_vlan'] in [10, 20, 30]
```

---

### 4.3 実務シナリオテンプレート集

#### テンプレート 1: BGP ピア状態監視 & ルート収束テスト
```yaml
name: "bgp-neighbor-established-check"
description: "BGPピアがESTABLISHED状態(6)になり、経路数が規定値を超えることを検証"
target_locks: ["leaf1"]
inputs:
  peer_ip:
    type: "string"
    default: "192.168.100.1"
steps:
  - id: "check_bgp_state"
    target: "leaf1"
    wait:
      # 6 = Established (BGP4-MIB::bgpPeerState)
      until: "raw['leaf1']['1.3.6.1.2.1.15.3.1.2.' + inputs.peer_ip] == 6"
      timeout: "30s"
      interval: "1s"
```

#### テンプレート 2: CPU・メモリ高負荷アラート検知テスト
```yaml
name: "cpu-load-threshold-check"
description: "CPU使用率が閾値以下に安定していることを確認"
target_locks: ["spine1"]
steps:
  - id: "check_cpu_utilization"
    target: "spine1"
    wait:
      until: "raw['spine1']['HOST-RESOURCES-MIB::hrProcessorLoad.1'] < 75"
      timeout: "15s"
      interval: "1s"
```

---

## 5. シナリオの登録・実行・進捗監視

### 5.1 シナリオの登録 (Import)

作成した YAML ファイルを Musubi サーバーに登録します。

```bash
# CLI によるシナリオ登録 (初回登録または更新)
./bin/musubi-cli scenarios import --file ./scenarios/linkdown.yaml
```

```bash
# REST API によるシナリオ登録
curl -X POST http://localhost:8080/v1/scenarios \
  -H "Content-Type: application/json" \
  -d '{
    "name": "spine-linkdown-failover-test",
    "description": "Spine1 Linkdown test",
    "dsl_yaml": "'"$(cat ./scenarios/linkdown.yaml | sed 's/"/\\"/g' | awk '{printf "%s\\n", $0}')"'"
  }'
```

---

### 5.2 シナリオの実行 (Run Job)

```bash
# CLI で実行を開始
./bin/musubi-cli scenarios run --name "spine-linkdown-failover-test"
```

#### パラメータを指定して REST API で実行
```bash
curl -X POST http://localhost:8080/v1/scenarios/spine-linkdown-failover-test/runs \
  -H "Content-Type: application/json" \
  -d '{
    "inputs": {
      "target_interface": "2",
      "wait_timeout": "15s"
    }
  }'
```
**レスポンス例 (`202 Accepted`):**
```json
{
  "job_id": "job-7b9a2c-174839201",
  "status": "QUEUED",
  "scenario_name": "spine-linkdown-failover-test",
  "scenario_version": 1,
  "created_at": "2026-08-22T14:30:00Z"
}
```

---

### 5.3 ジョブの進捗確認・ログ取得・強制キャンセル

```bash
# ジョブの詳細ステータス確認
./bin/musubi-cli jobs status --id "job-7b9a2c-174839201"

# ジョブのステップ実行ログの取得
./bin/musubi-cli jobs logs --id "job-7b9a2c-174839201"

# 実行中ジョブの安全な強制キャンセル (Teardownが自動実行されます)
./bin/musubi-cli jobs cancel --id "job-7b9a2c-174839201"
```

---

### 5.4 SSE (Server-Sent Events) によるリアルタイムストリーム購読

フロントエンドや CI/CD スクリプトから、状態変化やステップ進捗をリアルタイム購読できます。

```bash
# SSE ストリームの受信
curl -N http://localhost:8080/v1/events/stream
```

**受信イベント例:**
```json
data: {"id":"evt-101","topic":"job.step_advanced","payload":{"job_id":"job-1","step_id":"step1_verify_initial_link_up","status":"SUCCESS"},"timestamp":"2026-08-22T14:30:02Z"}

data: {"id":"evt-102","topic":"state.transition","payload":{"target":"spine1","state_key":"IF-MIB::ifOperStatus.1","old_value":"up","new_value":"down","trigger":"TRAP"},"timestamp":"2026-08-22T14:30:05Z"}
```

---

## 6. Grafana ダッシュボードによるリアルタイム監視

Musubi は VictoriaMetrics (TSDB) と連携した公式 Grafana ダッシュボード (`deploy/grafana/dashboards/musubi_overview.json`) を標準提供しています。

* **アクセス URL**: `http://localhost:3000/d/musubi-overview`
* **認証**: 認証なし (Anonymous 閲覧可能) または `admin / admin`

![Musubi Grafana Dashboard](images/grafana_dashboard.png)

### 提供パネル一覧
1. **リソース & 帯域監視**: CPU使用率、メモリ消費量、ネットワークスループット (Rx/Tx)。
2. **SNMP テレメトリ**: 受信 Trap/Inform レート、SNMP Request 処理レート、P95 レイテンシ。
3. **ターゲット MIB エクスプローラー**: ドロップダウンで機器（`spine1`, `spine2`等）を選択すると、取得済みの MIB ツリー値および Trap 履歴がリアルタイム更新。
4. **シナリオ & ジョブ稼働状況**: 実行中・完了ジョブの推移、排他リースロック中のターゲット一覧。

---

## 7. 運用・保守・ライフサイクル管理

### 7.1 孤立シナリオ (Orphan Scenario) の検知と自動クリーンアップ

ターゲット機器を削除・退役させた際、削除されたターゲットを参照しているシナリオ（孤立シナリオ）を自動検知して安全に解消できます。

```bash
# 1. 孤立シナリオの一覧確認
curl -s http://localhost:8080/v1/scenarios/orphans | jq .

# 2. 孤立シナリオの一括クリーンアップ
curl -s -X POST http://localhost:8080/v1/scenarios/cleanups | jq .
```

---

### 7.2 ターゲット機器の安全な削除フロー

```bash
# ステップ 1: ドレインを開始し、新規ジョブの割り当てを停止
./bin/musubi-cli targets drain --name "spine1"

# ステップ 2: 稼働中ジョブの完了を確認後、ターゲットを削除
./bin/musubi-cli targets delete --name "spine1"

# ※ 強制削除する場合 (実行中ジョブを強制アボート + 参照シナリオも自動クリーンアップ)
curl -X DELETE "http://localhost:8080/v1/targets/spine1?force=true&force_abort=true&cleanup_scenarios=true"
```

---

### 7.3 システムバックアップ & リストア

全ターゲット、認証プロファイル、シナリオ、バージョン履歴を JSON 形式で瞬時にバックアップ・リストア可能です。

```bash
# バックアップの作成
curl -s -X POST http://localhost:8080/v1/system/backups > musubi_backup_$(date +%Y%m%d).json

# バックアップからのリストア
curl -s -X POST http://localhost:8080/v1/system/restores \
  -H "Content-Type: application/json" \
  -d @musubi_backup_20260822.json
```

---

## 8. トラブルシューティング & FAQ

### 8.1 一般的なエラーコードと解決策

| HTTP ステータス | エラーコード (`Code`) | 主な原因 | 解決策 |
| :--- | :--- | :--- | :--- |
| `404 Not Found` | `TARGET_NOT_FOUND` | シナリオで指定されたターゲットが未登録 | ターゲットを先に作成するか、シナリオの `target_locks` の名前を確認してください。 |
| `409 Conflict` | `TARGET_IN_USE` | 対象機器が別ジョブによりロック中 | 先行ジョブの完了を待つか、不要な場合は `musubi-cli jobs cancel` でキャンセルしてください。 |
| `409 Conflict` | `SCENARIO_IN_USE` | 実行中ジョブがあるシナリオを削除しようとした | ジョブ完了を待つか、`?force_abort=true` を指定して削除してください。 |
| `422 Unprocessable`| `TARGET_DRAINING` | ターゲットがドレイン状態 | メンテナンス完了後にターゲットのステータスを `ONLINE` に戻してください。 |
| `422 Unprocessable`| `TARGET_MAINTENANCE`| ターゲットがメンテナンスモード | メンテナンス完了後にステータスを更新してください。 |
| `400 Bad Request` | `INVALID_YAML` | シナリオ YAML の構文エラー | インデントや必須フィールド（`name`, `steps`）を確認してください。 |
| `400 Bad Request` | `INVALID_CEL` | `wait.until` CEL 式の文法不正 | 括弧の対応や OID 文字列のクォート表記を確認してください。 |

---

### 8.2 よくある質問 (FAQ)

**Q. SNMP v1 や v2c の機器でも動作しますか？**  
A. はい。CredentialProfile で `version: "v1"` または `version: "v2c"` を指定し、Community 名を設定するだけで利用可能です。

**Q. ターゲット機器が 100 台以上ある環境でもスケールしますか？**  
A. はい。Musubi は Non-blocking 内部 Batcher と効率的な UDP Trap リスナーを備えており、128 台以上のターゲットセットや数万 Trap/秒の環境でも安定して稼働する設計になっています。

**Q. シナリオの途中でエラーが発生した場合、設定変更は元に戻りますか？**  
A. はい。シナリオに `teardown` ブロックを記述しておくことで、途中のステップが失敗またはタイムアウトした場合でも必ずロールバック処理が実行されます。

---

## 9. ユーザー体験 (UX/DX) 向上のためのベストプラクティス

### 9.1 GitOps 連携による自動試験パイプライン
シナリオ YAML を Git リポジトリ（例: `network-scenarios/`）で管理し、GitHub Actions や GitLab CI から `musubi-cli` を呼び出すことで、ネットワーク変更時の自動リグレッションテストが実現できます。

```yaml
# GitHub Actions のワークフロージョブ例
- name: Run Musubi Scenario Test
  run: |
    ./bin/musubi-cli scenarios import --file ./scenarios/bgp_check.yaml
    ./bin/musubi-cli scenarios run --name "bgp-neighbor-established-check"
```

### 9.2 Mock Agent を活用したローカルシミュレーション
実機が手元にない場合でも、同梱の `./bin/mock-snmp-agent` を起動しておくことで、開発マシン上で即座に Trap 発行や SNMP GET/SET の結合テストを実施できます。

---

*さらなる詳細な内部構造や拡張設計については [docs/architecture.md](architecture.md) および [docs/maintenance_guide.md](maintenance_guide.md) をご参照ください。*
