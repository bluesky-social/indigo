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
  PerSecondEventRate: RateLimit;
  PerHourEventRate: RateLimit;
  PerDayEventRate: RateLimit;
  RepoCount: number;
  RepoLimit: number;
  AccountLimitAlertsSilenced: boolean;
  AccountLimitAlertState: string;
  AccountLimitAlertSentAt?: string;
}

type PDSKey = keyof PDS;

export type { PDS, PDSKey };
