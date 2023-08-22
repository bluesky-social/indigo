interface RateLimit {
  MaxEventsPerSecond: number;
  TokenCount: number;
}

interface PDS {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  DeletedAt: any;
  Host: string;
  Did: string;
  SSL: boolean;
  Cursor: number;
  Registered: boolean;
  Blocked: boolean;
  HasActiveConnection: boolean;
  EventsSeenSinceStartup?: number;
  IngestRateLimit: RateLimit;
  CrawlRateLimit: RateLimit;
}

type PDSKey = keyof PDS;

export type { PDS, PDSKey };
