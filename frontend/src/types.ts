export interface Probe {
  State: string;
  Message: string;
  Checked: string;
  HTTPSMS: number;
  Mode?: string;
}
export interface Node {
  ID: number;
  SourceID: number;
  Name: string;
  Host: string;
  Port: string;
  Protocol: string;
  Transport: string;
  Security: string;
  Compatibility: string;
  Encryption: string;
  SNI: string;
  Flow: string;
  Path: string;
  Probe: Probe;
}
export interface Source {
  ID: number;
  Name: string;
  URL: string;
  Headers: string;
  Updated: string;
  Error: string;
  Interval: number;
  Count: number;
  Nodes: Node[];
  ProviderTitle: string;
  ProviderMessage: string;
  Usage: string;
  Limit: string;
  Expires: string;
  NextUpdate: string;
}
export interface Group {
  id: string;
  name: string;
  nodes: string[];
  interval: number;
  mode: string;
  threshold_ms: number;
  failures: number;
  cooldown: number;
}
export interface Rule {
  ID: number;
  Name: string;
  Kind: string;
  Value: string;
  Target: string;
  TargetLabel: string;
  Source: string;
  Disabled: boolean;
  ValueCount: number;
}
export interface Device {
  IP: string;
  MAC: string;
  Name: string;
  AutoName: string;
  Vendor: string;
  Seen: string;
  Target: string;
  TargetLabel: string;
  Manual: boolean;
}
export interface Network {
  interface: string;
  address: string;
  cidr: string;
  router: string;
}
export interface Component {
  Name: string;
  State: string;
  Class: string;
  Link: string;
  Action: string;
  Button: string;
  Hint: string;
  Update: string;
}
export interface RouteResult {
  Domain: string;
  Address: string;
  Source: string;
  Result: string;
  Target: string;
  TargetLabel: string;
  Reason: string;
  DNSReason: string;
  DNSLabel: string;
  DNSServers: string;
  Warning: string;
  Note: string;
}
export interface Page {
  Tab: string;
  PanelVersion: string;
  PanelCommit: string;
  Pending: boolean;
  ConfigError: string;
  GatewayReady: boolean;
  Wizard: boolean;
  Gateway: string;
  Mode: string;
  Selected: string;
  SelectedNode: Node | null;
  SourceCount: number;
  NodeCount: number;
  RuleCount: number;
  DeviceCount: number;
  Components: Component[];
  Sources: Source[];
  Nodes: Node[];
  Groups: Group[];
  Rules: Rule[];
  RuleOrder: string;
  Devices: Device[];
  DeviceDiscovery: string;
  Network: Network;
  DetectedNetwork: Network;
  NetworkError: string;
  DNS: string;
  DNSMode: string;
  DNSDirectServers: string;
  DNSVPNServers: string;
  Runtime: {
    State: string;
    Message: string;
    Network: string;
    ConfigHash: string;
    Gateway: boolean;
  };
  PanelUpdate: {
    State: string;
    Message: string;
    Available: boolean;
    CanRollback: boolean;
  };
  Version: string;
  Service: string;
  Routes: string;
  Memory: string;
  Uptime: string;
  RouteCheck: RouteResult | null;
  GeoError: string;
  GeoResults: { Kind: string; Code: string; Count: number; Example: string }[];
  GeoDetail: {
    Kind: string;
    Code: string;
    Example: string;
    Entries: string[];
  } | null;
  GeoNext: number;
  GeoOffset: number;
}
export interface GroupStatus {
  Name: string;
  Message: string;
  Policy: string;
  Controller: string;
  Samples: {
    NodeID: string;
    Alive: boolean;
    DelayMS: number;
    Checked: string;
  }[];
  Tag: string;
}
export type Values = Record<string, unknown>;
export interface ActionResult {
  ok: boolean;
  message: string;
  operation: string;
  action: string;
  since: string;
  pending: boolean;
  config_error: string;
}
