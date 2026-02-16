package pocket

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/pokt-network/sam/internal/models"
)

// Client performs read-only queries against the Pocket Network REST API.
type Client struct {
	HTTP   *http.Client
	Logger *slog.Logger
}

// maxResponseBody is the maximum size of an API response body (1 MB).
const maxResponseBody = 1 << 20

// NewClient returns a Client with a shared http.Client.
func NewClient(logger *slog.Logger) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				// Allow same-host redirects, block cross-host (SSRF)
				if req.URL.Host != via[0].URL.Host {
					return fmt.Errorf("redirect to different host blocked: %s", req.URL.Host)
				}
				return nil
			},
		},
		Logger: logger,
	}
}

// QueryBalance returns the uPOKT balance for an address.
func (c *Client) QueryBalance(address, apiEndpoint string) (int64, error) {
	url := fmt.Sprintf("%s/cosmos/bank/v1beta1/balances/%s", apiEndpoint, address)
	c.Logger.Debug("querying balance", "url", url)

	resp, err := c.HTTP.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to query balance API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return 0, fmt.Errorf("balance API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return 0, fmt.Errorf("failed to read balance response: %w", err)
	}

	var balanceResp models.APIBalanceResponse
	if err := json.Unmarshal(body, &balanceResp); err != nil {
		return 0, fmt.Errorf("failed to parse balance response: %w", err)
	}

	for _, coin := range balanceResp.Balances {
		if coin.Denom == "upokt" {
			balance, err := strconv.ParseInt(coin.Amount, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse balance amount: %w", err)
			}
			return balance, nil
		}
	}

	return 0, nil
}

// QueryApplication fetches application details and its liquid balance.
func (c *Client) QueryApplication(address, apiEndpoint, network string) (*models.Application, error) {
	url := fmt.Sprintf("%s/pokt-network/poktroll/application/application/%s", apiEndpoint, address)
	c.Logger.Debug("querying application", "url", url)

	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to query API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("application not found: %s", address)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp models.APIApplicationResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	app := &models.Application{
		Address: address,
		Network: network,
	}

	if apiResp.Application.Stake != nil {
		stakeAmount, err := strconv.ParseInt(apiResp.Application.Stake.Amount, 10, 64)
		if err != nil {
			c.Logger.Warn("failed to parse stake amount", "address", address, "error", err)
		} else {
			app.Stake = stakeAmount
			c.Logger.Debug("application stake", "address", address, "stake_upokt", stakeAmount)
		}
	}

	if len(apiResp.Application.ServiceConfigs) > 0 {
		sc := apiResp.Application.ServiceConfigs[0]
		if sc.Service != nil {
			app.ServiceID = sc.Service.ID
		} else {
			app.ServiceID = sc.ServiceID
		}
	}

	if len(apiResp.Application.DelegateeGatewayAddresses) > 0 {
		app.Gateway = apiResp.Application.DelegateeGatewayAddresses[0]
	}

	balance, err := c.QueryBalance(address, apiEndpoint)
	if err != nil {
		c.Logger.Warn("failed to query balance", "address", address, "error", err)
	} else {
		app.LiquidBalance = balance
	}

	return app, nil
}

// QueryServices returns available services on the network.
func (c *Client) QueryServices(apiEndpoint string) ([]models.ServiceInfo, error) {
	url := fmt.Sprintf("%s/pokt-network/poktroll/service/all_services", apiEndpoint)
	c.Logger.Debug("querying services", "url", url)

	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to query services API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("services API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read services response: %w", err)
	}

	var apiResp models.APIServicesResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse services response: %w", err)
	}

	services := make([]models.ServiceInfo, 0, len(apiResp.Service))
	for _, s := range apiResp.Service {
		services = append(services, models.ServiceInfo{
			ID:   s.ID,
			Name: s.Name,
		})
	}

	return services, nil
}

// QueryBankAccount returns the bank account balance for a network.
func (c *Client) QueryBankAccount(address, apiEndpoint, network string) (*models.BankAccount, error) {
	balance, err := c.QueryBalance(address, apiEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to query bank balance: %w", err)
	}

	return &models.BankAccount{
		Address: address,
		Balance: balance,
		Network: network,
	}, nil
}
