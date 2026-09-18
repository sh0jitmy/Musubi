# ADR 0003: Docker不要のスタンドアロンHTMXフロントエンドおよびSSG対応Goサーバの設計・導入

## ステータス
承認済み (Accepted)

## コンテキスト
これまでMusubiのテレメトリ可視化およびダッシュボード監視は、Docker Compose上のGrafana、VictoriaMetrics、およびPostgreSQLに依存していました。
しかし、本番環境の踏み台サーバや、セキュリティ要件によりコンテナランタイム（Docker）の導入が制限されるエアギャップ（閉域網）環境、あるいはローカルでの軽量な検証環境において、Docker不要（No-Docker）でMusubiの全可視化およびシナリオの入力・即時実行を行えるUIが強く求められていました。

SQLite対応（ADR 0001/0002）によりバックエンド自体はスタンドアロン起動が可能となったため、フロントエンドについても重量なSPAフレームワーク（React/Vue等）やDocker依存のGrafanaを排し、Go標準とHTMX、およびSSG（静的サイト生成）を組み合わせた軽量・堅牢なWebアーキテクチャを確立する必要がありました。

## 意思決定

以下の通り、独立したGoフロントエンドサーバおよびHTMX UIアーキテクチャを設計・導入しました。

1. **独立Goフロントエンドサーバ（`cmd/musubi-web` / `internal/web`）の分離**
   - バックエンドコア（`musubi-server`）のポート（デフォルト: 8080）と分離し、フロントエンドサーバ（デフォルト: 3001）として独立したバイナリを提供。
   - バックエンドのREST API（`/v1/...`）およびPrometheusエンドポイント（`/metrics`）をBFF（Backend for Frontend）クライアント経由で集約。

2. **HTMXによるHypermedia Drivenな動的UI構築**
   - 重大なクライアントサイドJavaScriptフレームワークを排し、HTML駆動のHTMXを採用。
   - 5秒間隔のポーリング（`hx-trigger="every 5s"`）およびイベント駆動の部品差し替え（`hx-swap="innerHTML" / "outerHTML"`）により、Grafanaと同等以上のリアルタイム可視化を実現。
   - エアギャップ環境に対応するため、HTMXライブラリ（`htmx.min.js`）およびCSSをGoの `embed.FS` でバイナリ内に完全自己完結させ、外部CDNへの一切の通信を排除。

3. **SSG（Static Site Generation: 静的サイト生成）機能の統合**
   - `musubi-web --ssg-export <dir>` および `make ssg-build` コマンドにより、テンプレートと初期データを事前レンダリングした静的HTML（`index.html`, `scenarios.html`）および静的アセットをファイルシステムへエクスポート可能に設計。
   - オフラインドキュメントやS3/GitHub Pages等の静的ストレージでの即時配布にも対応。

4. **Grafana全パネルの再現とシナリオ入力・実行スタジオの統合**
   - **システムリソース & 帯域**: Process CPU使用率、メモリ割り当て（Heap/Sys/RSS）、Active Goroutine数、SNMPトラフィックレート。
   - **ターゲットテレメトリ & MIB**: ターゲット一覧とリアルタイムヘルス（Ping/Drain操作可能）、最新MIBデータキャッシュ（Trap/Inform/BulkGet）、SNMP受信統計。
   - **シナリオ実行履歴 & 監査ログ**: ジョブ実行履歴、実行判定（SUCCESS/FAILED）、監査ログ。
   - **シナリオ入力・実行スタジオ**: YAML DSLエディタ、クイックプリセット（sysDescr, IF Admin Down/Up, BulkGet）自動挿入、即時実行トリガー、リアルタイムジョブ進捗追跡。

5. **ヘッドレスChromeによるE2Eテストとスナップショット自動生成（`make frontend-e2e`）**
   - `scripts/test_frontend_ui.py` および `scripts/frontend_e2e.sh` により、Docker不要で全システムを立ち上げて全HTMXコンポーネント・シナリオ登録実行を自動検証。
   - 解像度1920x1280の高品質スナップショット（`docs/images/frontend_dashboard.png`, `docs/images/frontend_scenarios.png`）を撮影し、単一HTMLレポート（`test_reports/frontend_e2e_report.html`）を生成。

## 影響・結果

- **Docker不要での完全稼働**: `mock-snmp-agent`、`musubi-server`（SQLite）、`musubi-web` の3バイナリのみで、コンテナなしの環境でフル機能のネットワークオーケストレーションと監視が稼働。
- **高可用性とポータビリティ**: メモリ使用量は数MB〜数十MBに収まり、起動も1秒未満で完了。
- **監査証跡の自動蓄積**: E2Eテストにより、Grafanaと同様の客観的スナップショット画像とテストレポートが自動生成され、品質保証プロセスの完全性を証明。
