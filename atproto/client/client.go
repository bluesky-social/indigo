package client

type AuthInfo struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	AdminToken string `json:"adminToken"`
	Handle     string `json:"handle"`
	Did        string `json:"did"`
}

type Client struct {
	Client     *http.Client
	Auth       *AuthInfo
	Host       string
	UserAgent  *string
	Headers    map[string]string
}

// Context can include additional headers, including "cache busting", metrics collection, etc.

func (c *Client) Login(ctx context.Context, ident syntax.AtIdentifier, password string) (AuthInfo, error) {
}


func (c *Client) Get(ctx context.Context, endpoint string, params map[string]interface{}) (map[string]interface{}, error) {
}

func (c *Client) GetBytes(ctx context.Context, endpoint string, params map[string]interface{}) ([]byte, error) {
}

func (c *Client) Post(ctx context.Context, endpoint string, body, params map[string]interface{}) (map[string]interface{}, error) {
}

// TODO: return stdlib HTTP wrapper?
func (c *Client) PostBytes(ctx context.Context, endpoint string, contentType string, body, params map[string]interface{}) ([]byte, error) {
}

type StreamEvent struct {
	// header meta
	// body bytes
}

func (c *Client) Subscribe(ctx context.Context, endpoint string, c chan[StreamEvent], params map[string]interface{}) error
