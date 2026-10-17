# Traffic accounting

The optional `ngpanel-traffic.service` runs the embedded `--collect-traffic` collector. The web process only reads its sanitized snapshot at `/run/ngpanel-traffic/traffic.json`, exposed through `/api/traffic`.

## Measurements

- Devices: IPv4 source/destination counters in the independent `inet ngpanel_traffic` nftables table. Upload is counted on the LAN interface after TPROXY marking; download is counted on packets emitted by the Xray UID toward LAN clients with the original remote source address. TCP and UDP are included. Local panel traffic and the VM's own requests are excluded. DNS intercepted separately on port 53 is outside these device counters.
- Outbounds: Linux nftables counters for each dialling Direct/VPN outbound. A deterministic unique SO_MARK identifies each outbound; its low 16 bits retain bypass value 666. Output rules save the socket mark into conntrack and count original-direction packets. Prerouting rules count reply-direction packets by that conntrack mark. These are actual IP bytes (including TCP acknowledgements, transport overhead, retransmissions and VM/probe traffic), not application payload or exclusively client traffic. Group members stay separate. Counting remains continuous with Xray splice enabled; StatsService is not the source of these rates.
- The traffic table is separate from gateway verdict rules. Gateway bypass checks the low 16 bits of packet marks; policy routing still matches only mark 1. No routes or traffic destinations are changed. Configurations without unique marks report unavailable accounting until applied; rates are never inferred from aggregate device totals.
- Route animation: additional expiring nft sets count TCP/UDP IPv4 five-tuples on the client side. The collector joins the exact source IP/port, destination IP/port and protocol to actual Xray access events, never to a guessed route or a connection-count ratio. Distinct or ambiguous outbound matches remain unattributed. Flow counters expire after 90 seconds and route baselines are in memory; first samples after a collector/table restart have zero rates.
- Particle departures share a bounded budget across current routes, allocated in proportion to measured upload/download byte rates. A fast payload stream and its small acknowledgement stream cannot independently hit the same density cap. Each particle starts at a node and finishes its immutable journey even if the measured rate becomes zero. Numeric speeds remain unsmoothed and are shown in node details, never on the graph links. A single particle follows the complete known device–gateway–outbound path (in reverse for downloads). Unattributed traffic stops at the gateway. Each connection uses one shared line for both particle directions. All particles have the same radius and solid fill: light lavender for uploads and purple for downloads travelling in reverse, including VPN checks. Links with active TCP sessions retain constant colour and brightness between transfers. Links without sessions, measured transfer or particles in flight are dimmed and dashed; only travelling particles indicate measured transfer. Socket rows without an attributed outbound contribute to a separate pending-attribution note, never a guessed graph exit. Missing or stale samples never fall back to connection counts. Reduced-motion preferences suppress particles.

Device identity for accounting is the IPv4 address, not a permanent MAC identity. IP reassignment can therefore combine usage at that address. Only traffic passing through this VM is visible. Current per-device/per-outbound route rates are provided; permanent usage totals remain separate device and outbound totals. Very short or missing journal events can leave traffic unattributed; the UI does not assign it to an arbitrary exit. Observatory results and manual checks produce separate, identified gateway–VPN request/response animations. These represent confirmed check events, not inferred byte volumes or exact packet timing. Other unattributed VM/outbound transport traffic is not silently assigned to a device or labelled as a probe.

## Persistence and lifecycle

Cumulative totals and source baselines live in `/var/lib/ngpanel-traffic/history.db`. One-second samples produce rates; SQLite checkpoints run every ten samples and on graceful shutdown. A sudden simultaneous loss of the collector and underlying counters can lose the last uncheckpointed interval. No historical usage before accounting was enabled is reconstructed. Table generations and falling counters are handled without double counting or rate spikes; the first rate after collector startup is zero. During migration from Xray transport counters, accumulated historical totals are retained and subsequent IP bytes are added; the differing byte definitions make this a usage indicator, not billing data.

The route join reads the existing sanitized connection journal, using the fixed loopback API if the journal owner permissions prevent direct access; no extra DAC capability is granted.

The dedicated service needs CAP_NET_ADMIN for nftables and CAP_CHOWN for the group-readable snapshot; these capabilities are not granted to the panel or connection journal collector. Its accounting rules contain no drop/reject verdicts and do not replace the gateway table. The installer and fixed root `traffic-install` operation provision it. The updater stops it before binary replacement and disables it when rolling back to pre-v0.3.40 releases. During the unreleased v0.3.39 experiment it is installed manually by the fixed operation.

## Verification

`go test ./...` checks counter generations, resets, stale snapshots and parser fixtures. With `NG_XRAY_INTEGRATION=1`, generated core configuration is validated by the installed Xray and the fixed root validator.

Generate an accounting rules fixture using:

```
NG_TRAFFIC_FIXTURE=/tmp/ng-traffic-fixture.nft go test -run TestTrafficNFTFixture
sudo unshare --net python3 deploy/test-traffic-lab.py /tmp/ng-traffic-fixture.nft
NG_TRAFFIC_REAL_NFT=/tmp/ng-traffic-lab-counters.json go test -run TestTrafficRealNFTFixture
```

The lab creates disposable client/gateway/WAN namespaces. It transfers known HTTP payloads and UDP datagrams through Direct and a test VLESS server, checks both device counter directions and outbound counters, then checks one-second samples during sustained downloads and uploads through both Direct and VPN before sockets close. Real nft five-tuples are additionally matched against the lab core access log for both exits. It checks idle intervals for delayed bursts and verifies that gateway-local traffic is excluded. It never accesses the production DB or core configuration. Live deployment additionally checks collector restart persistence and that the applied core configuration changes only outbound marks, preserving all routing and acceleration settings.

### Map interactions

The graph keeps a fixed 438 px canvas with no internal scrollbars. All devices and exits are shown simultaneously around the gateway, on arcs for small groups and in staggered bands for dense groups. Card sizes adapt to the distance between centres; dense layouts use compact cards or icons, with names and details available on hover and selection. There are no pages or internal scrollbars. Hover or keyboard focus highlights related device/outbound paths from observed TCP and measured routes; selecting a node keeps its path highlighted while details are open. Device details link to the active TCP list with an exact source-IP filter. Names are saved in the device record through `device-name`, which does not rewrite routing rules; icon choices remain in this browser. VPN outlines use recent real check results (green/red), becoming neutral when checks are missing or older than two minutes. Hover text includes delay and check time.

### Lightweight polling

The map reads devices from `/api/map/devices` rather than building the routing editor page. Home requests `/api/connections?active=1`; the full connections/history response remains available unchanged. The traffic collector's loopback fallback uses `?raw=1` to read the recorded tuples without server-side UI label enrichment or a redundant decode/encode. Source timestamps and existing stale-data checks are retained. Probe status and applied outbound names use narrow SQL projections instead of parsing subscription links. Theme settings load minimal page metadata. No polling intervals or particle timing were changed.

On the copied-database stand, 66 requests over 30 seconds consumed 4.38% of one core before and 2.72% after, with mean response times 70.0 ms and 17.07 ms (no request errors). This isolates the request workload with fixed replayed snapshots; it is not a measurement of all production services or browser rendering.

### История графика главной

Сборщик трафика ведёт ограниченную историю последних 90 секунд (`TrafficSnapshot.History`, максимум 96 точек). Используются только суммы счётчиков устройств, без повторного учёта исходящих и без дополнительных команд Linux. Измерения продолжаются, когда главная закрыта или браузер свёрнут. При недоступных счётчиках сохраняется пропуск, а не вымышленный ноль.

История включена в существующий снимок `/api/traffic` и сохраняется вместе со счётчиками в `meta.chart-history` существующей БД учёта раз в 10 секунд и при штатной остановке сборщика. В браузере она объединяется с кратким локальным кешем, который остаётся запасным вариантом; серверные измерения имеют приоритет. Это восстанавливает график после переключения разделов, перезагрузки страницы и повторного открытия панели.
