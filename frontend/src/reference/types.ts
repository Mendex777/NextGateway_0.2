export interface Status {
  netIO: { up: number; down: number };
  netTraffic: { sent: number; recv: number };
  tcpCount: number;
  udpCount: number;
}
