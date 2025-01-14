package common

type WorkRequest struct {
	ID   string `json:"id" db:"id"`
	Data string `json:"data" db:"request_data"`
	Done bool   `json:"done" db:"done"`
}

type ProofResponse struct {
	ID    string `json:"id"`
	Proof []byte `json:"proof"`
	Error string `json:"error"`
}

type WorkResponse struct {
	Work     bool          `json:"work"`
	Requests []WorkRequest `json:"requests"`
}
