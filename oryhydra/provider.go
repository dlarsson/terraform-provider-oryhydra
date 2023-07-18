package oryhydra

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"

	httptransport "github.com/go-openapi/runtime/client"
	_ "github.com/go-sql-driver/mysql"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	hydra "github.com/ory/hydra-client-go/client"
	"github.com/ory/hydra-client-go/client/admin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"url": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("ORY_HYDRA_URL", nil),
			},
			"dbhost": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("ORY_HYDRA_DB_HOST", nil),
			},
			"dbuser": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("ORY_HYDRA_DB_USER", nil),
			},
			"dbpassword": {
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("ORY_HYDRA_DB_PASSWORD", nil),
			},
			"oauth2_token_url": {
				Type:         schema.TypeString,
				Optional:     true,
				RequiredWith: []string{"oauth2_client_id", "oauth2_client_secret"},
				DefaultFunc:  schema.EnvDefaultFunc("ORY_HYDRA_OAUTH2_TOKEN_URL", nil),
			},
			"oauth2_client_id": {
				Type:         schema.TypeString,
				Optional:     true,
				RequiredWith: []string{"oauth2_token_url", "oauth2_client_secret"},
				DefaultFunc:  schema.EnvDefaultFunc("ORY_HYDRA_OAUTH2_CLIENT_ID", nil),
			},
			"oauth2_client_secret": {
				Type:         schema.TypeString,
				Optional:     true,
				RequiredWith: []string{"oauth2_client_id", "oauth2_token_url"},
				DefaultFunc:  schema.EnvDefaultFunc("ORY_HYDRA_OAUTH2_CLIENT_SECRET", nil),
			},
			"header_authorization": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("HEADER_AUTHORIZATION", nil),
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"oryhydra_oauth2_client": resourceOAuth2Client(),
		},
		ConfigureFunc: configure,
	}
}

type HydraClient struct {
	Hydra admin.ClientService
	DB    *sql.DB
}

func configure(data *schema.ResourceData) (interface{}, error) {
	adminURL := data.Get("url").(string)
	dbHost := data.Get("dbhost").(string)
	dbUser := data.Get("dbuser").(string)
	dbPasswd := data.Get("dbpassword").(string)
	dbConnectString := fmt.Sprintf("%s:%s@tcp(%s)/hydra", dbUser, dbPasswd, dbHost)

	hydra := new(HydraClient)

	if db, err := sql.Open("mysql", dbConnectString); err != nil {
		return nil, fmt.Errorf("Failed to connect to %s/hydra as %s", dbHost, dbUser)
	} else {
		hydra.DB = db
		defer db.Close()
	}

	httpClient := cleanhttp.DefaultClient()

	if tokenURL, ok := data.GetOk("oauth2_token_url"); ok {
		config := clientcredentials.Config{
			TokenURL:     tokenURL.(string),
			ClientID:     data.Get("oauth2_client_id").(string),
			ClientSecret: data.Get("oauth2_client_secret").(string),
		}
		ctx := context.WithValue(context.TODO(), oauth2.HTTPClient, httpClient)
		httpClient = config.Client(ctx)
	} else if header, ok := data.GetOk("header_authorization"); ok {
		httpClient.Transport = &authHeaderTransport{
			origin: httpClient.Transport,
			header: header.(string),
		}
	}

	client, err := newHydraClient(adminURL, httpClient)
	hydra.Hydra = client
	return hydra, err
}

// newHydraClient returns a new configured hydra client.
func newHydraClient(hydraAdminURL string, httpClient *http.Client) (admin.ClientService, error) {
	u, err := url.Parse(hydraAdminURL)
	if err != nil {
		return nil, fmt.Errorf("parse hydra url: %v", err)
	}

	config := hydra.DefaultTransportConfig()
	config.Schemes = []string{u.Scheme}
	config.Host = u.Host
	if u.Path != "" {
		config.BasePath = u.Path
	}

	transport := httptransport.NewWithClient(
		config.Host,
		config.BasePath,
		config.Schemes,
		httpClient,
	)

	client := hydra.New(transport, nil)
	return client.Admin, nil
}

type authHeaderTransport struct {
	origin http.RoundTripper
	header string
}

func (a *authHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", a.header)
	return a.origin.RoundTrip(req)
}
