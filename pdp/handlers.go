package pdp

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"
)

///////////////////////////////////////
//////////////////////////////////////
/////// NOTE NOTE NOTE NOTE NOTE ////
//// This is an empty skeleton created
//// by AI with no proof-read. Please
//// do not think this is close to anything final at all

// PDPRoutePath is the base path for PDP routes
const PDPRoutePath = "/pdp"

// PDPService represents the service for managing proof sets and pieces
type PDPService struct {
	ProofSetStore     ProofSetStore
	PieceStore        PieceStore
	OwnerAddressStore OwnerAddressStore
}

// NewPDPService creates a new instance of PDPService with the provided stores
func NewPDPService(proofSetStore ProofSetStore, pieceStore PieceStore, ownerAddressStore OwnerAddressStore) *PDPService {
	return &PDPService{
		ProofSetStore:     proofSetStore,
		PieceStore:        pieceStore,
		OwnerAddressStore: ownerAddressStore,
	}
}

// Routes registers the HTTP routes with the provided router
func Routes(r *chi.Mux, p *PDPService) {

	// Routes for proof sets
	r.Route(path.Join(PDPRoutePath, "/proof-sets"), func(r chi.Router) {
		// POST /pdp/proof-sets - Create a new proof set
		r.Post("/", p.handleCreateProofSet)

		// Individual proof set routes
		r.Route("/{proofSetID}", func(r chi.Router) {
			// GET /pdp/proof-sets/{set-id}
			r.Get("/", p.handleGetProofSet)

			// DEL /pdp/proof-sets/{set-id}
			r.Delete("/", p.handleDeleteProofSet)

			// Routes for roots within a proof set
			r.Route("/roots", func(r chi.Router) {
				// POST /pdp/proof-sets/{set-id}/roots
				r.Post("/", p.handleAddRootToProofSet)

				// Individual root routes
				r.Route("/{rootID}", func(r chi.Router) {
					// GET /pdp/proof-sets/{set-id}/roots/{root-id}
					r.Get("/", p.handleGetProofSetRoot)

					// DEL /pdp/proof-sets/{set-id}/roots/{root-id}
					r.Delete("/", p.handleDeleteProofSetRoot)
				})
			})
		})
	})

	// Routes for piece storage and retrieval
	// POST /pdp/piece
	r.Post(path.Join(PDPRoutePath, "/piece"), p.handlePiecePost)

	// PUT /pdp/piece/upload/{pieceCID}
	r.Put(path.Join(PDPRoutePath, "/piece/upload/{pieceCID}"), p.handlePieceUpload)

	// GET /pdp/piece/{pieceCid}
	r.Get(path.Join(PDPRoutePath, "/piece/{pieceCid}"), p.handleGetPiece)

	// HEAD /pdp/piece/{pieceCid}
	r.Head(path.Join(PDPRoutePath, "/piece/{pieceCid}"), p.handleHeadPiece)
}

// Handler functions

func (p *PDPService) handleCreateProofSet(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### POST /proof-sets
	// Create a new proof set
	// Request Body:
	// {
	//     "ownerAddress": "f3..."
	// }
	// Response:
	// Code: 201
	// Location header: "/proof-sets/{set-id}"

	// Parse request body
	var req struct {
		OwnerAddress string `json:"ownerAddress"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || req.OwnerAddress == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Implement authorization
	ownerExists, err := p.OwnerAddressStore.HasOwnerAddress(req.OwnerAddress)
	if err != nil {
		http.Error(w, "Failed to check owner address", http.StatusInternalServerError)
		return
	}
	if !ownerExists {
		http.Error(w, "Owner address not recognized", http.StatusUnauthorized)
		return
	}

	// Create the proof set
	proofSet := &PDPProofSet{
		// ID will be assigned by the store (on-chain proof set ID)
		NextChallengeEpoch: 0, // Initial value; will be updated upon proving
	}

	// Create the proof set in the store
	proofSetID, err := p.ProofSetStore.CreateProofSet(proofSet)
	if err != nil {
		http.Error(w, "Failed to create proof set", http.StatusInternalServerError)
		return
	}

	// Set Location header
	w.Header().Set("Location", path.Join(PDPRoutePath, "/proof-sets", fmt.Sprintf("%d", proofSetID)))

	// Set status code to 201 Created
	w.WriteHeader(http.StatusCreated)
}

func (p *PDPService) handleGetProofSet(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### GET /proof-sets/{set-id}
	// Response:
	// Code: 200
	// Body:
	// {
	//   "id": "{set-id}",
	//   "nextChallengeEpoch": 15,
	//   "roots": [
	//     // Root details
	//   ]
	// }

	proofSetIDStr := chi.URLParam(r, "proofSetID")
	proofSetID, err := strconv.ParseInt(proofSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID", http.StatusBadRequest)
		return
	}

	// Retrieve proof set from store
	proofSetDetails, err := p.ProofSetStore.GetProofSet(proofSetID)
	if err != nil {
		http.Error(w, "Proof set not found", http.StatusNotFound)
		return
	}

	// Implement authorization if necessary

	// Respond with proof set details
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(proofSetDetails)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (p *PDPService) handleDeleteProofSet(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### DEL /proof-sets/{set id}
	// Remove the specified proof set entirely

	proofSetIDStr := chi.URLParam(r, "proofSetID")
	proofSetID, err := strconv.ParseInt(proofSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID", http.StatusBadRequest)
		return
	}

	// Implement authorization (e.g., only the owner can delete)

	err = p.ProofSetStore.DeleteProofSet(proofSetID)
	if err != nil {
		http.Error(w, "Failed to delete proof set", http.StatusInternalServerError)
		return
	}

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

func (p *PDPService) handleAddRootToProofSet(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### POST /proof-sets/{set-id}/roots
	// Append a root to the proof set
	// Request Body:
	// {
	//   "rootId": {root ID},
	//   "rootCid": "bafy....root",
	//   "subroots": [
	//     {
	//       "subrootCid": "bafy...subroot",
	//       "subrootOffset": 0,
	//       "pieceCid": "bafy...piece1"
	//     },
	//     //...
	//   ]
	// }

	proofSetIDStr := chi.URLParam(r, "proofSetID")
	proofSetID, err := strconv.ParseInt(proofSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		RootID   int64  `json:"rootId"`
		RootCID  string `json:"rootCid"`
		Subroots []struct {
			SubrootCID    string `json:"subrootCid"`
			SubrootOffset int64  `json:"subrootOffset"`
			PieceCID      string `json:"pieceCid"`
		} `json:"subroots"`
	}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil || req.RootCID == "" || len(req.Subroots) == 0 {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Implement authorization (e.g., only owner can add roots)

	// For each subroot, check that the piece exists and get the pdp_pieceref ID
	for _, subroot := range req.Subroots {
		// Check if piece exists
		exists, err := p.PieceStore.HasPiece(subroot.PieceCID)
		if err != nil {
			http.Error(w, "Error checking piece existence", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "Piece not found: "+subroot.PieceCID, http.StatusBadRequest)
			return
		}

		// Get the pdp_pieceref ID for this piece
		pieceRefID, err := p.PieceStore.GetPieceRefIDByPieceCID(subroot.PieceCID)
		if err != nil {
			http.Error(w, "Failed to get piece reference for "+subroot.PieceCID, http.StatusInternalServerError)
			return
		}

		// Create the proof set root entry
		proofSetRoot := &PDPProofSetRoot{
			ProofSetID:    proofSetID,
			RootID:        req.RootID,
			Root:          req.RootCID,
			Subroot:       subroot.SubrootCID,
			SubrootOffset: subroot.SubrootOffset,
			PDPPieceRefID: pieceRefID,
		}

		// Add to proof set store
		err = p.ProofSetStore.AddProofSetRoot(proofSetRoot)
		if err != nil {
			http.Error(w, "Failed to add root to proof set", http.StatusInternalServerError)
			return
		}
	}

	// Set Location header
	w.Header().Set("Location", path.Join(PDPRoutePath, "/proof-sets", fmt.Sprintf("%d", proofSetID), "roots", fmt.Sprintf("%d", req.RootID)))

	// Set status code to 201 Created
	w.WriteHeader(http.StatusCreated)
}

func (p *PDPService) handleGetProofSetRoot(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### GET /proof-sets/{set id}/roots/{root id}
	// Response Body:
	// {
	//   "rootId": {root ID},
	//   "rootCid": "bafy....root",
	//   "subroots": [
	//     {
	//       "subrootCid": "bafy...subroot",
	//       "subrootOffset": 0,
	//       "pieceCid": "bafy...piece1"
	//     },
	//     //...
	//   ]
	// }

	proofSetIDStr := chi.URLParam(r, "proofSetID")
	proofSetID, err := strconv.ParseInt(proofSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID", http.StatusBadRequest)
		return
	}

	rootIDStr := chi.URLParam(r, "rootID")
	rootID, err := strconv.ParseInt(rootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid root ID", http.StatusBadRequest)
		return
	}

	// Retrieve root from proof set in store
	rootDetails, err := p.ProofSetStore.GetProofSetRoot(proofSetID, rootID)
	if err != nil {
		http.Error(w, "Root not found", http.StatusNotFound)
		return
	}

	// Respond with root details
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(rootDetails)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (p *PDPService) handleDeleteProofSetRoot(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### DEL /proof-sets/{set id}/roots/{root id}

	proofSetIDStr := chi.URLParam(r, "proofSetID")
	proofSetID, err := strconv.ParseInt(proofSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID", http.StatusBadRequest)
		return
	}

	rootIDStr := chi.URLParam(r, "rootID")
	rootID, err := strconv.ParseInt(rootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid root ID", http.StatusBadRequest)
		return
	}

	// Implement authorization (e.g., only owner can delete roots)

	// Delete root from proof set in store
	err = p.ProofSetStore.DeleteProofSetRoot(proofSetID, rootID)
	if err != nil {
		http.Error(w, "Failed to delete root", http.StatusInternalServerError)
		return
	}

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

func (p *PDPService) handlePiecePost(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### POST /piece
	// Request Body:
	// {
	//   "pieceCid": "{piece cid v2}",
	//   "refId": "{ref ID}",
	//   "serviceId": {service ID},
	//   "serviceTag": "optional service tag",
	//   "clientTag": "optional client tag",
	//   "notify": "optional http webhook to call once the data is uploaded"
	// }

	// Implement authorization (e.g., verify service identity)

	// Parse request body
	var req struct {
		PieceCID   string `json:"pieceCid"`
		RefID      string `json:"refId"`
		ServiceID  int64  `json:"serviceId"`
		ServiceTag string `json:"serviceTag,omitempty"`
		ClientTag  string `json:"clientTag,omitempty"`
		Notify     string `json:"notify,omitempty"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || req.PieceCID == "" || req.RefID == "" || req.ServiceID == 0 {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if piece is already stored
	exists, err := p.PieceStore.HasPiece(req.PieceCID)
	if err != nil {
		http.Error(w, "Failed to check piece existence", http.StatusInternalServerError)
		return
	}

	if exists {
		// Return 204 No Content
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Create a location URL where the piece data can be uploaded via PUT
	uploadURL := path.Join(PDPRoutePath, "/piece/upload", req.PieceCID)

	// Store the piece reference
	pieceRef := &PDPPieceRef{
		ServiceID:  req.ServiceID,
		PieceCID:   req.PieceCID,
		RefID:      req.RefID,
		ServiceTag: req.ServiceTag,
		ClientTag:  req.ClientTag,
		CreatedAt:  time.Now(),
	}
	err = p.PieceStore.CreatePieceRef(pieceRef)
	if err != nil {
		http.Error(w, "Failed to create piece reference", http.StatusInternalServerError)
		return
	}

	// TODO: Store the notify URL associated with the pieceCID if provided

	// Return 201 Created with Location header
	w.Header().Set("Location", uploadURL)
	w.WriteHeader(http.StatusCreated)
}

func (p *PDPService) handlePieceUpload(w http.ResponseWriter, r *http.Request) {
	// Handles the PUT request to upload the actual bytes of the piece

	pieceCID := chi.URLParam(r, "pieceCID")

	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the piece data from request body
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read piece data", http.StatusInternalServerError)
		return
	}

	// Verify that the data hashes to the provided piece CID (using V2 Piece CID hashing)
	verified, err := VerifyPieceData(pieceCID, data)
	if err != nil {
		http.Error(w, "Failed to verify piece data: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !verified {
		http.Error(w, "Piece data does not match the provided piece CID", http.StatusBadRequest)
		return
	}

	// Store the piece data
	err = p.PieceStore.StorePiece(pieceCID, data)
	if err != nil {
		http.Error(w, "Failed to store piece", http.StatusInternalServerError)
		return
	}

	// Optionally, call the notify webhook if set
	// TODO: Retrieve the notify URL associated with the pieceCID and send notification

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

func (p *PDPService) handleGetPiece(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### GET /piece/{piece cid v2}

	pieceCID := chi.URLParam(r, "pieceCid")

	// Retrieve the piece data from PieceStore
	data, err := p.PieceStore.GetPiece(pieceCID)
	if err != nil {
		http.Error(w, "Piece not found", http.StatusNotFound)
		return
	}

	// Serve the piece data with appropriate headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)
	if err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}

func (p *PDPService) handleHeadPiece(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### HEAD /piece/{piece cid v2}

	pieceCID := chi.URLParam(r, "pieceCid")

	// Check if the piece exists in PieceStore
	exists, err := p.PieceStore.HasPiece(pieceCID)
	if err != nil {
		http.Error(w, "Failed to check piece existence", http.StatusInternalServerError)
		return
	}

	if !exists {
		http.Error(w, "Piece not found", http.StatusNotFound)
		return
	}

	// Return headers indicating the piece exists
	w.WriteHeader(http.StatusOK)
}

// Data models corresponding to the updated schema

// PDPOwnerAddress represents the owner address with its private key
type PDPOwnerAddress struct {
	OwnerAddress string // PRIMARY KEY
	PrivateKey   []byte // BYTEA NOT NULL
}

// PDPServiceEntry represents a PDP service entry
type PDPServiceEntry struct {
	ID           int64     // PRIMARY KEY
	PublicKey    []byte    // BYTEA NOT NULL
	ServiceLabel string    // TEXT NOT NULL
	CreatedAt    time.Time // DEFAULT CURRENT_TIMESTAMP
}

// PDPPieceRef represents a PDP piece reference
type PDPPieceRef struct {
	ID         int64     // PRIMARY KEY
	ServiceID  int64     // pdp_services.id
	PieceCID   string    // TEXT NOT NULL
	RefID      string    // TEXT NOT NULL
	ServiceTag string    // VARCHAR(64)
	ClientTag  string    // VARCHAR(64)
	CreatedAt  time.Time // DEFAULT CURRENT_TIMESTAMP
}

// PDPProofSet represents a proof set
type PDPProofSet struct {
	ID                 int64 // PRIMARY KEY (on-chain proofset id)
	NextChallengeEpoch int64 // Cached chain value
}

// PDPProofSetRoot represents a root in a proof set
type PDPProofSetRoot struct {
	ProofSetID    int64  // proofset BIGINT NOT NULL
	RootID        int64  // root_id BIGINT NOT NULL
	Root          string // root TEXT NOT NULL
	Subroot       string // subroot TEXT NOT NULL
	SubrootOffset int64  // subroot_offset BIGINT NOT NULL
	PDPPieceRefID int64  // pdp_piecerefs.id
}

// PDPProveTask represents a prove task
type PDPProveTask struct {
	ProofSetID     int64  // proofset
	ChallengeEpoch int64  // challenge epoch
	TaskID         int64  // harmonytask task ID
	MessageCID     string // text
	MessageEthHash string // text
}

// Interfaces

// ProofSetStore defines methods to manage proof sets and roots
type ProofSetStore interface {
	CreateProofSet(proofSet *PDPProofSet) (int64, error)
	GetProofSet(proofSetID int64) (*PDPProofSetDetails, error)
	DeleteProofSet(proofSetID int64) error
	AddProofSetRoot(proofSetRoot *PDPProofSetRoot) error
	GetProofSetRoot(proofSetID int64, rootID int64) (*PDPProofSetRootDetails, error)
	DeleteProofSetRoot(proofSetID int64, rootID int64) error
}

// PieceStore defines methods to manage pieces and piece references
type PieceStore interface {
	HasPiece(pieceCID string) (bool, error)
	StorePiece(pieceCID string, data []byte) error
	GetPiece(pieceCID string) ([]byte, error)
	CreatePieceRef(pieceRef *PDPPieceRef) error
	GetPieceRefIDByPieceCID(pieceCID string) (int64, error)
}

// OwnerAddressStore defines methods to manage owner addresses
type OwnerAddressStore interface {
	HasOwnerAddress(ownerAddress string) (bool, error)
}

// PDPProofSetDetails represents the details of a proof set, including roots
type PDPProofSetDetails struct {
	ID                 int64                   `json:"id"`
	NextChallengeEpoch int64                   `json:"nextChallengeEpoch"`
	Roots              []PDPProofSetRootDetail `json:"roots"`
}

// PDPProofSetRootDetail represents the details of a root in a proof set
type PDPProofSetRootDetail struct {
	RootID   int64                      `json:"rootId"`
	RootCID  string                     `json:"rootCid"`
	Subroots []PDPProofSetSubrootDetail `json:"subroots"`
}

// PDPProofSetSubrootDetail represents a subroot in a proof set root
type PDPProofSetSubrootDetail struct {
	SubrootCID    string `json:"subrootCid"`
	SubrootOffset int64  `json:"subrootOffset"`
	PieceCID      string `json:"pieceCid"`
}

// Helper function to verify piece data against its CID
func VerifyPieceData(pieceCID string, data []byte) (bool, error) {
	// TODO: Implement verification using the appropriate hashing function for Piece CID v2
	return true, nil
}