interface Consumer {
  RemoteAddr: string;
  UserAgent: string;
  EventsConsumed: number;
  ConnectedAt: Date;
  ID: number;
}

interface ConsumerResponse {
  id: number;
  remote_addr: string;
  user_agent: string;
  events_consumed: number;
  connected_at: string;
}

type ConsumerKey = keyof Consumer;

export type { Consumer, ConsumerResponse, ConsumerKey };
