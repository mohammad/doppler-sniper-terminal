package indexerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"doppler-sniper/internal/models"
)

const DefaultGraphQLEndpoint = "https://testnet-indexer.doppler.lol/graphql"
const maxSwapPageSize = 500

type Repository struct {
	endpoint string
	client   *http.Client
}

func New(endpoint string) *Repository {
	if endpoint == "" {
		endpoint = DefaultGraphQLEndpoint
	}
	return &Repository{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type graphQLRequest struct {
	Query string `json:"query"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse[T any] struct {
	Data   T              `json:"data"`
	Errors []graphQLError `json:"errors"`
}

func (r *Repository) do(ctx context.Context, query string, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query})
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute graphql request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read graphql response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("graphql status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	return nil
}

func firstError[T any](resp graphQLResponse[T]) error {
	if len(resp.Errors) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(resp.Errors))
	for _, err := range resp.Errors {
		msgs = append(msgs, err.Message)
	}
	return fmt.Errorf(strings.Join(msgs, "; "))
}

func (r *Repository) Ping(ctx context.Context) error {
	type metaData struct {
		Meta struct {
			Status map[string]any `json:"status"`
		} `json:"_meta"`
	}

	var resp graphQLResponse[metaData]
	if err := r.do(ctx, `query { _meta { status } }`, &resp); err != nil {
		return err
	}
	return firstError(resp)
}

func (r *Repository) HasRequiredSchema(context.Context) (bool, error) {
	return true, nil
}

func (r *Repository) CountCheckpoints(context.Context) (int, error) {
	return 1, nil
}

func (r *Repository) IsIndexerReady(context.Context) (bool, error) {
	return true, nil
}

func (r *Repository) ListMigratedLaunches(ctx context.Context, filter models.LaunchFilter) ([]models.Launch, error) {
	chainID := 0
	if len(filter.ChainIDs) > 0 {
		chainID = filter.ChainIDs[0]
	}

	whereParts := make([]string, 0, 4)
	if chainID > 0 {
		whereParts = append(whereParts, fmt.Sprintf("chainId: %d", chainID))
	}

	switch filter.CurveType {
	case "", "all":
		if chainID == 8453 {
			whereParts = append(whereParts, `type: "zora"`)
		}
	case "multicurve":
		whereParts = append(whereParts, `type_contains: "multicurve"`)
	case "standard-v4":
		whereParts = append(whereParts, `type_in: ["v4", "v4-decay"]`)
	case "v3":
		whereParts = append(whereParts, `type: "v3"`)
	case "zora":
		whereParts = append(whereParts, `type: "zora"`)
	}

	if filter.DateRange != "" && filter.DateRange != "all" {
		days := map[string]int{"7d": 7, "30d": 30, "90d": 90}[filter.DateRange]
		if days > 0 {
			whereParts = append(whereParts, fmt.Sprintf("lastSwapTimestamp_gte: \"%d\"", time.Now().AddDate(0, 0, -days).Unix()))
		}
	}

	whereParts = append(whereParts, "lastSwapTimestamp_not: null")

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
query {
  pools(
    where: { %s }
    orderBy: "lastSwapTimestamp"
    orderDirection: "desc"
    limit: %d
  ) {
    items {
      address
      chainId
      type
      createdAt
      migratedAt
      totalTokensSold
      asset { address }
      baseToken { symbol name image }
    }
  }
}`, strings.Join(whereParts, ", "), limit)

	type launchItem struct {
		Address         string `json:"address"`
		ChainID         int    `json:"chainId"`
		Type            string `json:"type"`
		CreatedAt       string `json:"createdAt"`
		MigratedAt      string `json:"migratedAt"`
		TotalTokensSold string `json:"totalTokensSold"`
		Asset           struct {
			Address string `json:"address"`
		} `json:"asset"`
		BaseToken struct {
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
			Image  string `json:"image"`
		} `json:"baseToken"`
	}
	type launchesData struct {
		Pools struct {
			Items []launchItem `json:"items"`
		} `json:"pools"`
	}

	var resp graphQLResponse[launchesData]
	if err := r.do(ctx, query, &resp); err != nil {
		return nil, err
	}
	if err := firstError(resp); err != nil {
		return nil, fmt.Errorf("list launches: %w", err)
	}

	launches := make([]models.Launch, 0, len(resp.Data.Pools.Items))
	for _, item := range resp.Data.Pools.Items {
		launches = append(launches, models.Launch{
			Address:         strings.ToLower(item.Address),
			ChainID:         item.ChainID,
			Asset:           strings.ToLower(item.Asset.Address),
			Type:            item.Type,
			CreatedAt:       parseInt64(item.CreatedAt),
			MigratedAt:      parseInt64(item.MigratedAt),
			TotalTokensSold: parseInt64(item.TotalTokensSold),
			Symbol:          item.BaseToken.Symbol,
			Name:            item.BaseToken.Name,
			Image:           item.BaseToken.Image,
		})
	}

	return launches, nil
}

func (r *Repository) GetLaunchByAsset(ctx context.Context, assetAddress string, chainID int) (models.Launch, error) {
	query := fmt.Sprintf(`
query {
  token(address: "%s", chainId: %d) {
    address
    symbol
    name
    pool {
      address
      chainId
      type
      createdAt
      migratedAt
      totalTokensSold
      baseToken {
        symbol
        name
        image
      }
    }
  }
}`, strings.ToLower(assetAddress), chainID)

	type data struct {
		Token *struct {
			Address string `json:"address"`
			Symbol  string `json:"symbol"`
			Name    string `json:"name"`
			Pool    *struct {
				Address         string `json:"address"`
				ChainID         int    `json:"chainId"`
				Type            string `json:"type"`
				CreatedAt       string `json:"createdAt"`
				MigratedAt      string `json:"migratedAt"`
				TotalTokensSold string `json:"totalTokensSold"`
				BaseToken       struct {
					Symbol string `json:"symbol"`
					Name   string `json:"name"`
					Image  string `json:"image"`
				} `json:"baseToken"`
			} `json:"pool"`
		} `json:"token"`
	}

	var resp graphQLResponse[data]
	if err := r.do(ctx, query, &resp); err != nil {
		return models.Launch{}, err
	}
	if err := firstError(resp); err != nil {
		return models.Launch{}, fmt.Errorf("get asset launch: %w", err)
	}
	if resp.Data.Token == nil || resp.Data.Token.Pool == nil {
		return models.Launch{}, fmt.Errorf("%w: %s", ErrAssetLookupFailed, strings.ToLower(assetAddress))
	}

	token := resp.Data.Token
	pool := token.Pool
	symbol := firstNonEmpty(pool.BaseToken.Symbol, token.Symbol)
	name := firstNonEmpty(pool.BaseToken.Name, token.Name)

	return models.Launch{
		Address:         strings.ToLower(pool.Address),
		ChainID:         pool.ChainID,
		Asset:           strings.ToLower(token.Address),
		Type:            pool.Type,
		CreatedAt:       parseInt64(pool.CreatedAt),
		MigratedAt:      parseInt64(pool.MigratedAt),
		TotalTokensSold: parseInt64(pool.TotalTokensSold),
		Symbol:          symbol,
		Name:            name,
		Image:           pool.BaseToken.Image,
	}, nil
}

func (r *Repository) CountLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) (int, error) {
	swapArgs := `limit: 1`
	if migratedAt > 0 {
		swapArgs = fmt.Sprintf(`where: { timestamp_lte: "%d" }, limit: 1`, migratedAt)
	}

	query := fmt.Sprintf(`
query {
  pool(address: "%s", chainId: %d) {
    swaps(%s) {
      totalCount
    }
  }
}`, strings.ToLower(poolAddress), chainID, swapArgs)

	type data struct {
		Pool *struct {
			Swaps struct {
				TotalCount int `json:"totalCount"`
			} `json:"swaps"`
		} `json:"pool"`
	}

	var resp graphQLResponse[data]
	if err := r.do(ctx, query, &resp); err != nil {
		return 0, err
	}
	if err := firstError(resp); err != nil {
		return 0, fmt.Errorf("count swaps: %w", err)
	}
	if resp.Data.Pool == nil {
		return 0, nil
	}
	return resp.Data.Pool.Swaps.TotalCount, nil
}

func (r *Repository) GetLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) ([]models.Swap, error) {
	type swapItem struct {
		TxHash       string `json:"txHash"`
		ChainID      int    `json:"chainId"`
		User         string `json:"user"`
		Type         string `json:"type"`
		AmountIn     string `json:"amountIn"`
		AmountOut    string `json:"amountOut"`
		SwapValueUSD string `json:"swapValueUsd"`
		Timestamp    string `json:"timestamp"`
	}
	type data struct {
		Pool *struct {
			Swaps struct {
				Items []swapItem `json:"items"`
			} `json:"swaps"`
		} `json:"pool"`
	}

	swaps := make([]models.Swap, 0, maxSwapPageSize)
	for offset := 0; ; offset += maxSwapPageSize {
		swapArgs := fmt.Sprintf(`orderBy: "timestamp", orderDirection: "asc", limit: %d, offset: %d`, maxSwapPageSize, offset)
		if migratedAt > 0 {
			swapArgs = fmt.Sprintf(`where: { timestamp_lte: "%d" }, orderBy: "timestamp", orderDirection: "asc", limit: %d, offset: %d`, migratedAt, maxSwapPageSize, offset)
		}

		query := fmt.Sprintf(`
query {
  pool(address: "%s", chainId: %d) {
    swaps(%s) {
      items {
        txHash
        chainId
        user
        type
        amountIn
        amountOut
        swapValueUsd
        timestamp
      }
    }
  }
}`, strings.ToLower(poolAddress), chainID, swapArgs)

		var resp graphQLResponse[data]
		if err := r.do(ctx, query, &resp); err != nil {
			return nil, err
		}
		if err := firstError(resp); err != nil {
			return nil, fmt.Errorf("get swaps: %w", err)
		}
		if resp.Data.Pool == nil || len(resp.Data.Pool.Swaps.Items) == 0 {
			break
		}

		for _, item := range resp.Data.Pool.Swaps.Items {
			timestamp := parseInt64(item.Timestamp)
			if migratedAt > 0 && timestamp > migratedAt {
				continue
			}
			swaps = append(swaps, models.Swap{
				TxHash:       item.TxHash,
				Pool:         strings.ToLower(poolAddress),
				ChainID:      item.ChainID,
				User:         strings.ToLower(item.User),
				Type:         item.Type,
				AmountIn:     parseInt64(item.AmountIn),
				AmountOut:    parseInt64(item.AmountOut),
				SwapValueUSD: parseInt64(item.SwapValueUSD),
				Timestamp:    timestamp,
			})
		}

		if len(resp.Data.Pool.Swaps.Items) < maxSwapPageSize {
			break
		}
	}

	return swaps, nil
}

func (r *Repository) StreamLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64, pageSize int, consume func([]models.Swap) error) error {
	if pageSize <= 0 {
		pageSize = maxSwapPageSize
	}

	type swapItem struct {
		TxHash       string `json:"txHash"`
		ChainID      int    `json:"chainId"`
		User         string `json:"user"`
		Type         string `json:"type"`
		AmountIn     string `json:"amountIn"`
		AmountOut    string `json:"amountOut"`
		SwapValueUSD string `json:"swapValueUsd"`
		Timestamp    string `json:"timestamp"`
	}
	type data struct {
		Pool *struct {
			Swaps struct {
				Items []swapItem `json:"items"`
			} `json:"swaps"`
		} `json:"pool"`
	}

	for offset := 0; ; offset += pageSize {
		swapArgs := fmt.Sprintf(`orderBy: "timestamp", orderDirection: "asc", limit: %d, offset: %d`, pageSize, offset)
		if migratedAt > 0 {
			swapArgs = fmt.Sprintf(`where: { timestamp_lte: "%d" }, orderBy: "timestamp", orderDirection: "asc", limit: %d, offset: %d`, migratedAt, pageSize, offset)
		}

		query := fmt.Sprintf(`
query {
  pool(address: "%s", chainId: %d) {
    swaps(%s) {
      items {
        txHash
        chainId
        user
        type
        amountIn
        amountOut
        swapValueUsd
        timestamp
      }
    }
  }
}`, strings.ToLower(poolAddress), chainID, swapArgs)

		var resp graphQLResponse[data]
		if err := r.do(ctx, query, &resp); err != nil {
			return err
		}
		if err := firstError(resp); err != nil {
			return fmt.Errorf("stream swaps: %w", err)
		}
		if resp.Data.Pool == nil || len(resp.Data.Pool.Swaps.Items) == 0 {
			break
		}

		page := make([]models.Swap, 0, len(resp.Data.Pool.Swaps.Items))
		for _, item := range resp.Data.Pool.Swaps.Items {
			timestamp := parseInt64(item.Timestamp)
			if migratedAt > 0 && timestamp > migratedAt {
				continue
			}
			page = append(page, models.Swap{
				TxHash:       item.TxHash,
				Pool:         strings.ToLower(poolAddress),
				ChainID:      item.ChainID,
				User:         strings.ToLower(item.User),
				Type:         item.Type,
				AmountIn:     parseInt64(item.AmountIn),
				AmountOut:    parseInt64(item.AmountOut),
				SwapValueUSD: parseInt64(item.SwapValueUSD),
				Timestamp:    timestamp,
			})
		}

		if len(page) > 0 {
			if err := consume(page); err != nil {
				return err
			}
		}

		if len(resp.Data.Pool.Swaps.Items) < pageSize {
			break
		}
	}
	return nil
}

func (r *Repository) GetLatestSwapTimestamp(ctx context.Context) (int64, error) {
	query := `
query {
  swaps(orderBy: "timestamp", orderDirection: "desc", limit: 1) {
    items {
      timestamp
    }
  }
}`

	type data struct {
		Swaps struct {
			Items []struct {
				Timestamp string `json:"timestamp"`
			} `json:"items"`
		} `json:"swaps"`
	}

	var resp graphQLResponse[data]
	if err := r.do(ctx, query, &resp); err != nil {
		return 0, err
	}
	if err := firstError(resp); err != nil {
		return 0, fmt.Errorf("latest swap: %w", err)
	}
	if len(resp.Data.Swaps.Items) == 0 {
		return 0, nil
	}
	return parseInt64(resp.Data.Swaps.Items[0].Timestamp), nil
}

func parseInt64(value string) int64 {
	if value == "" {
		return 0
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		return n
	}
	return 0
}

var ErrAssetLookupFailed = errors.New("asset lookup failed")

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
