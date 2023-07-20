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
}

type PDSKey = keyof PDS;

export type { PDS, PDSKey };
