package common

type WorkRequest struct {
	ID   int64 `json:"id" db:"id"`

	Data *string `json:"data" db:"request_data"`
	Done *bool   `json:"done" db:"done"`

	WorkAskID int64 `json:"work_ask_id" db:"work_ask_id"`
}

type ProofResponse struct {
	ID    string `json:"id"`
	Proof []byte `json:"proof"`
	Error string `json:"error"`
}

type WorkResponse struct {
	Requests []WorkRequest `json:"requests"`
	ActiveAsks []int64 `json:"active_asks"`
}

type WorkAsk struct {
	ID int64 `json:"id"`
}
