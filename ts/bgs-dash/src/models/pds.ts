interface RateLimit {
  Max: number;
  WindowSeconds: number;
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
  CrawlRate: RateLimit;
  PerSecondEventRate: RateLimit;
  PerHourEventRate: RateLimit;
  PerDayEventRate: RateLimit;
  RepoCount: number;
  RepoLimit: number;
}

type PDSKey = keyof PDS;

export type { PDS, PDSKey };
