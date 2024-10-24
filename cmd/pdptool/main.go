package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/golang-jwt/jwt/v4"
	"github.com/minio/sha256-simd"
	"github.com/urfave/cli/v2"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"

	curiobuild "github.com/filecoin-project/curio/build"
)

func main() {
	app := &cli.App{
		Name:    "pdptool",
		Usage:   "tool for testing PDP capabilities",
		Version: curiobuild.UserVersion(),
		Commands: []*cli.Command{
			authCreateServiceSecretCmd, // generates pdpservice.json, outputs pubkey
			authCreateJWTTokenCmd,      // generates jwt token from a secret

			pingCmd,

			piecePrepareCmd, // hash a piece to get a piece cid
			pieceUploadCmd,  // upload a piece to a pdp service

			createProofSetCmd,    // create a new proof set on the PDP service
			getProofSetStatusCmd, // get the status of a proof set creation on the PDP service
			getProofSetCmd,       // retrieve the details of a proof set from the PDP service

			addRootsCmd,
		},
	}
	app.Setup()

	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

var authCreateServiceSecretCmd = &cli.Command{
	Name:  "create-service-secret",
	Usage: "Generate a new service secret and public key",
	Action: func(cctx *cli.Context) error {
		// Generate an ECDSA private key
		privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.New(rand.NewSource(time.Now().UnixNano())))
		if err != nil {
			return fmt.Errorf("failed to generate private key: %v", err)
		}

		// Serialize the private key to PEM
		privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
		if err != nil {
			return fmt.Errorf("failed to marshal private key: %v", err)
		}
		privPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privBytes,
		})

		// Serialize the public key to PEM
		pubBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
		if err != nil {
			return fmt.Errorf("failed to marshal public key: %v", err)
		}
		pubPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubBytes,
		})

		// Save the private key to pdpservice.json
		serviceSecret := map[string]string{
			"private_key": string(privPEM),
		}

		file, err := os.OpenFile("pdpservice.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("failed to open pdpservice.json for writing: %v", err)
		}
		defer file.Close()
		encoder := json.NewEncoder(file)
		if err := encoder.Encode(&serviceSecret); err != nil {
			return fmt.Errorf("failed to write to pdpservice.json: %v", err)
		}

		// Output the public key
		fmt.Printf("Public Key:\n%s\n", pubPEM)

		return nil
	},
}

var authCreateJWTTokenCmd = &cli.Command{
	Name:      "create-jwt-token",
	Usage:     "Generate a JWT token using the service secret",
	ArgsUsage: "[service_name]",
	Action: func(cctx *cli.Context) error {
		// Read the private key from pdpservice.json
		privKey, err := loadPrivateKey()
		if err != nil {
			return err
		}

		// Get the service name
		serviceName := cctx.Args().First()
		if serviceName == "" {
			return fmt.Errorf("service_name argument is required")
		}

		// Create JWT token using the common function
		tokenString, err := createJWTToken(serviceName, privKey)
		if err != nil {
			return err
		}

		// Output the token
		fmt.Printf("JWT Token:\n%s\n", tokenString)

		return nil
	},
}

var pingCmd = &cli.Command{
	Name:  "ping",
	Usage: "Ping the /pdp/ping endpoint of a PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "service-name",
			Usage: "Service Name to include in the JWT token",
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")

		if serviceName == "" {
			return fmt.Errorf("either --jwt-token or --service-name must be provided")
		}
		privKey, err := loadPrivateKey()
		if err != nil {
			return err
		}
		var errCreateToken error
		jwtToken, errCreateToken := createJWTToken(serviceName, privKey)
		if errCreateToken != nil {
			return errCreateToken
		}

		// Append /pdp/ping to the service URL
		pingURL := serviceURL + "/pdp/ping"

		// Create the GET request
		req, err := http.NewRequest("GET", pingURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check the response
		if resp.StatusCode == http.StatusOK {
			color.Green("Ping successful: Service is reachable and JWT token is valid.")
		} else {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("ping failed with status code %d: %s", resp.StatusCode, string(body))
		}

		return nil
	},
}

func createJWTToken(serviceName string, privateKey *ecdsa.PrivateKey) (string, error) {
	// Create JWT claims
	claims := jwt.MapClaims{
		"service_name": serviceName,
		"exp":          time.Now().Add(time.Hour * 24).Unix(),
	}

	// Create the token
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)

	// Sign the token
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %v", err)
	}

	return tokenString, nil
}

func loadPrivateKey() (*ecdsa.PrivateKey, error) {
	file, err := os.Open("pdpservice.json")
	if err != nil {
		return nil, fmt.Errorf("failed to open pdpservice.json: %v", err)
	}
	defer file.Close()
	var serviceSecret map[string]string
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&serviceSecret); err != nil {
		return nil, fmt.Errorf("failed to read pdpservice.json: %v", err)
	}

	privPEM := serviceSecret["private_key"]
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to parse private key PEM")
	}

	// Parse the private key
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}
	ecdsaPrivKey, ok := privKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ECDSA")
	}

	return ecdsaPrivKey, nil
}

var piecePrepareCmd = &cli.Command{
	Name:      "prepare-piece",
	Usage:     "Compute the PieceCID of a file",
	ArgsUsage: "<input-file>",
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		inputFile := cctx.Args().Get(0)
		if inputFile == "" {
			return fmt.Errorf("input file is required")
		}

		// Open input file
		file, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open input file: %v", err)
		}
		defer file.Close()

		// Get the piece size from flag or use file size
		fi, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat input file: %v", err)
		}

		pieceSize := fi.Size()

		// Create commp calculator
		cp := &commp.Calc{}

		// Copy data into commp calculator
		_, err = io.Copy(cp, file)
		if err != nil {
			return fmt.Errorf("failed to read input file: %v", err)
		}

		// Finalize digest
		digest, paddedPieceSize, err := cp.Digest()
		if err != nil {
			return fmt.Errorf("failed to compute digest: %v", err)
		}

		// Convert digest to CID
		pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
		if err != nil {
			return fmt.Errorf("failed to compute piece CID: %v", err)
		}

		// now compute sha256
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek file: %v", err)
		}

		// Create commp calculator
		h := sha256.New()
		_, err = io.Copy(h, file)
		if err != nil {
			return fmt.Errorf("failed to read input file: %v", err)
		}

		// Finalize digest
		shadigest := h.Sum(nil)

		// Output the piece CID and size
		fmt.Printf("Piece CID: %s\n", pieceCIDComputed)
		fmt.Printf("SHA256: %x\n", shadigest)
		fmt.Printf("Padded Piece Size: %d bytes\n", paddedPieceSize)
		fmt.Printf("Raw Piece Size: %d bytes\n", pieceSize)

		return nil
	},
}

var pieceUploadCmd = &cli.Command{
	Name:      "upload-piece",
	Usage:     "Upload a piece to a PDP service",
	ArgsUsage: "<input-file>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "jwt-token",
			Usage: "JWT token for authentication (optional if --service-name is provided)",
		},
		&cli.StringFlag{
			Name:  "service-name",
			Usage: "Service Name to include in the JWT token (used if --jwt-token is not provided)",
		},
		&cli.StringFlag{
			Name:     "notify-url",
			Usage:    "Notification URL",
			Required: false,
		},
		&cli.StringFlag{
			Name:  "hash-type",
			Usage: "Hash type to use for verification (sha256 or commp)",
			Value: "sha256",
		},
		&cli.BoolFlag{
			Name:  "local-notif-wait",
			Usage: "Wait for server notification by spawning a temporary local HTTP server",
		},
	},
	Action: func(cctx *cli.Context) error {
		inputFile := cctx.Args().Get(0)
		if inputFile == "" {
			return fmt.Errorf("input file is required")
		}

		serviceURL := cctx.String("service-url")
		jwtToken := cctx.String("jwt-token")
		notifyURL := cctx.String("notify-url")
		serviceName := cctx.String("service-name")
		hashType := cctx.String("hash-type")
		localNotifWait := cctx.Bool("local-notif-wait")

		if jwtToken == "" {
			if serviceName == "" {
				return fmt.Errorf("either --jwt-token or --service-name must be provided")
			}
			privKey, err := loadPrivateKey()
			if err != nil {
				return err
			}
			var errCreateToken error
			jwtToken, errCreateToken = createJWTToken(serviceName, privKey)
			if errCreateToken != nil {
				return errCreateToken
			}
		}

		if hashType != "sha256" && hashType != "commp" {
			return fmt.Errorf("invalid hash type: %s", hashType)
		}

		if localNotifWait && notifyURL != "" {
			return fmt.Errorf("cannot specify both --notify-url and --local-notif-wait")
		}

		var notifyReceived chan struct{}
		var server *http.Server
		var ln net.Listener

		if localNotifWait {
			notifyReceived = make(chan struct{})
			var err error
			ln, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return fmt.Errorf("failed to start local HTTP server: %v", err)
			}
			serverAddr := fmt.Sprintf("http://%s/notify", ln.Addr().String())
			notifyURL = serverAddr

			mux := http.NewServeMux()
			mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
				fmt.Println("Received notification from server.")
				b, err := io.ReadAll(r.Body)
				if err != nil {
					fmt.Printf("Failed to read notification body: %v\n", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				fmt.Printf("Notification body: %s\n", string(b))
				w.WriteHeader(http.StatusOK)
				// Signal that notification was received
				close(notifyReceived)
			})

			server = &http.Server{Handler: mux}

			go func() {
				if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
					fmt.Printf("HTTP server error: %v\n", err)
				}
			}()

			defer func() {
				server.Close()
				ln.Close()
			}()
		}

		// Open input file
		file, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open input file: %v", err)
		}
		defer file.Close()

		// Get the piece size
		fi, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat input file: %v", err)
		}
		pieceSize := fi.Size()

		// Compute CommP (PieceCID)
		cp := &commp.Calc{}
		_, err = io.Copy(cp, file)
		if err != nil {
			return fmt.Errorf("failed to read input file: %v", err)
		}

		commpDigest, _, err := cp.Digest()
		if err != nil {
			return fmt.Errorf("failed to compute digest: %v", err)
		}

		// Compute SHA256
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek file: %v", err)
		}

		h := sha256.New()
		_, err = io.Copy(h, file)
		if err != nil {
			return fmt.Errorf("failed to read input file: %v", err)
		}

		shadigest := h.Sum(nil)

		// Prepare the check data
		var checkData map[string]interface{}

		switch hashType {
		case "sha256":
			checkData = map[string]interface{}{
				"name": "sha2-256",
				"hash": hex.EncodeToString(shadigest),
				"size": pieceSize,
			}
		case "commp":
			hashHex := hex.EncodeToString(commpDigest)
			checkData = map[string]interface{}{
				"name": "sha2-256-trunc254-padded",
				"hash": hashHex,
				"size": pieceSize,
			}
		default:
			return fmt.Errorf("unsupported hash type: %s", hashType)
		}

		// Prepare the request data
		reqData := map[string]interface{}{
			"check": checkData,
		}
		if notifyURL != "" {
			reqData["notify"] = notifyURL
		}
		reqBody, err := json.Marshal(reqData)
		if err != nil {
			return fmt.Errorf("failed to marshal request data: %v", err)
		}

		client := &http.Client{}

		req, err := http.NewRequest("POST", serviceURL+"/pdp/piece", bytes.NewReader(reqBody))
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Piece already exists, get the pieceCID from the response
			var respData map[string]string
			err = json.NewDecoder(resp.Body).Decode(&respData)
			if err != nil {
				return fmt.Errorf("failed to parse response: %v", err)
			}
			pieceCID := respData["pieceCID"]
			fmt.Printf("Piece already exists on the server. Piece CID: %s\n", pieceCID)
			return nil
		} else if resp.StatusCode == http.StatusCreated {
			// Get the upload URL from the Location header
			uploadURL := resp.Header.Get("Location")
			if uploadURL == "" {
				return fmt.Errorf("server did not provide upload URL in Location header")
			}

			// Upload the piece data via PUT
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("failed to seek file: %v", err)
			}
			uploadReq, err := http.NewRequest("PUT", serviceURL+uploadURL, file)
			if err != nil {
				return fmt.Errorf("failed to create upload request: %v", err)
			}
			// Set the Content-Length header
			uploadReq.ContentLength = pieceSize
			// Set the Content-Type header
			uploadReq.Header.Set("Content-Type", "application/octet-stream")

			uploadResp, err := client.Do(uploadReq)
			if err != nil {
				return fmt.Errorf("failed to upload piece data: %v", err)
			}
			defer uploadResp.Body.Close()

			if uploadResp.StatusCode != http.StatusNoContent {
				body, _ := io.ReadAll(uploadResp.Body)
				return fmt.Errorf("upload failed with status code %d: %s", uploadResp.StatusCode, string(body))
			}

			fmt.Println("Piece uploaded successfully.")

			if localNotifWait {
				fmt.Println("Waiting for server notification...")
				<-notifyReceived
			}

			return nil
		} else {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server returned status code %d: %s", resp.StatusCode, string(body))
		}
	},
}

var createProofSetCmd = &cli.Command{
	Name:  "create-proof-set",
	Usage: "Create a new proof set on the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "recordkeeper",
			Usage:    "Address of the record keeper contract",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")
		recordKeeper := cctx.String("recordkeeper")

		// Load the private key (implement `loadPrivateKey` according to your setup)
		privKey, err := loadPrivateKey()
		if err != nil {
			return fmt.Errorf("failed to load private key: %v", err)
		}

		// Create the JWT token (implement `createJWTToken` according to your setup)
		jwtToken, err := createJWTToken(serviceName, privKey)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Construct the request payload
		requestBody := map[string]string{
			"recordKeeper": recordKeeper,
		}
		requestBodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Append /pdp/proof-sets to the service URL
		postURL := serviceURL + "/pdp/proof-sets"

		// Create the POST request
		req, err := http.NewRequest("POST", postURL, bytes.NewBuffer(requestBodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", "application/json")

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Read and display the response
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %v", err)
		}
		bodyString := string(bodyBytes)

		if resp.StatusCode == http.StatusCreated {
			location := resp.Header.Get("Location")
			fmt.Printf("Proof set creation initiated successfully.\n")
			fmt.Printf("Location: %s\n", location)
			fmt.Printf("Response: %s\n", bodyString)
		} else {
			return fmt.Errorf("failed to create proof set, status code %d: %s", resp.StatusCode, bodyString)
		}

		return nil
	},
}

var getProofSetStatusCmd = &cli.Command{
	Name:  "get-proof-set-create-status",
	Usage: "Get the status of a proof set creation on the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "tx-hash",
			Usage:    "Transaction hash of the proof set creation",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")
		txHash := cctx.String("tx-hash")

		// Load the private key
		privKey, err := loadPrivateKey()
		if err != nil {
			return fmt.Errorf("failed to load private key: %v", err)
		}

		// Create the JWT token
		jwtToken, err := createJWTToken(serviceName, privKey)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Ensure txHash starts with '0x'
		if !strings.HasPrefix(txHash, "0x") {
			txHash = "0x" + txHash
		}
		txHash = strings.ToLower(txHash) // Ensure txHash is in lowercase

		// Construct the request URL
		getURL := fmt.Sprintf("%s/pdp/proof-sets/created/%s", serviceURL, txHash)

		// Create the GET request
		req, err := http.NewRequest("GET", getURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Read and process the response
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %v", err)
		}

		if resp.StatusCode == http.StatusOK {
			// Decode the JSON response
			var response struct {
				CreateMessageHash string  `json:"createMessageHash"`
				ProofsetCreated   bool    `json:"proofsetCreated"`
				Service           string  `json:"service"`
				TxStatus          string  `json:"txStatus"`
				OK                *bool   `json:"ok"`
				ProofSetId        *uint64 `json:"proofSetId,omitempty"`
			}
			err = json.Unmarshal(bodyBytes, &response)
			if err != nil {
				return fmt.Errorf("failed to parse JSON response: %v", err)
			}

			// Display the status
			fmt.Printf("Proof Set Creation Status:\n")
			fmt.Printf("Transaction Hash: %s\n", response.CreateMessageHash)
			fmt.Printf("Transaction Status: %s\n", response.TxStatus)
			if response.OK != nil {
				fmt.Printf("Transaction Successful: %v\n", *response.OK)
			} else {
				fmt.Printf("Transaction Successful: Pending\n")
			}
			fmt.Printf("Proofset Created: %v\n", response.ProofsetCreated)
			if response.ProofSetId != nil {
				fmt.Printf("ProofSet ID: %d\n", *response.ProofSetId)
			}
		} else {
			return fmt.Errorf("failed to get proof set status, status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		return nil
	},
}

var getProofSetCmd = &cli.Command{
	Name:      "get-proof-set",
	Usage:     "Retrieve the details of a proof set from the PDP service",
	ArgsUsage: "<set-id>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		// Parse arguments
		setIDStr := cctx.Args().Get(0)
		if setIDStr == "" {
			return fmt.Errorf("set-id argument is required")
		}

		// Parse setID to uint64
		setID, err := strconv.ParseUint(setIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid set-id format: %v", err)
		}

		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")

		// Load the private key
		privKey, err := loadPrivateKey()
		if err != nil {
			return fmt.Errorf("failed to load private key: %v", err)
		}

		// Create the JWT token
		jwtToken, err := createJWTToken(serviceName, privKey)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Construct the request URL
		getURL := fmt.Sprintf("%s/pdp/proof-sets/%d", serviceURL, setID)

		// Create the GET request
		req, err := http.NewRequest("GET", getURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Read and process the response
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %v", err)
		}

		if resp.StatusCode == http.StatusOK {
			// Decode the JSON response
			var response struct {
				ID                 uint64 `json:"id"`
				NextChallengeEpoch int64  `json:"nextChallengeEpoch"`
				Roots              []struct {
					RootID        uint64 `json:"rootId"`
					RootCID       string `json:"rootCid"`
					SubrootCID    string `json:"subrootCid"`
					SubrootOffset int64  `json:"subrootOffset"`
				} `json:"roots"`
			}
			err = json.Unmarshal(bodyBytes, &response)
			if err != nil {
				return fmt.Errorf("failed to parse JSON response: %v", err)
			}

			// Display the proof set details
			fmt.Printf("Proof Set ID: %d\n", response.ID)
			fmt.Printf("Next Challenge Epoch: %d\n", response.NextChallengeEpoch)
			fmt.Printf("Roots:\n")
			for _, root := range response.Roots {
				fmt.Printf("  - Root ID: %d\n", root.RootID)
				fmt.Printf("    Root CID: %s\n", root.RootCID)
				fmt.Printf("    Subroot CID: %s\n", root.SubrootCID)
				fmt.Printf("    Subroot Offset: %d\n", root.SubrootOffset)
				fmt.Println()
			}
		} else {
			return fmt.Errorf("failed to get proof set, status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		return nil
	},
}

var addRootsCmd = &cli.Command{
	Name:  "add-roots",
	Usage: "Add roots to a proof set on the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:     "proof-set-id",
			Usage:    "ID of the proof set to which roots will be added",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:     "root",
			Usage:    "Root CID and its subroots. Format: rootCID:subrootCID1+subrootCID2,...",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")
		proofSetID := cctx.Uint64("proof-set-id")
		rootInputs := cctx.StringSlice("root")

		// Load the private key (implement `loadPrivateKey` according to your setup)
		privKey, err := loadPrivateKey()
		if err != nil {
			return fmt.Errorf("failed to load private key: %v", err)
		}

		// Create the JWT token (implement `createJWTToken` according to your setup)
		jwtToken, err := createJWTToken(serviceName, privKey)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Parse the root inputs to construct the request payload
		type SubrootEntry struct {
			SubrootCID string `json:"subrootCid"`
		}

		type AddRootRequest struct {
			RootCID  string         `json:"rootCid"`
			Subroots []SubrootEntry `json:"subroots"`
		}

		var addRootRequests []AddRootRequest

		for _, rootInput := range rootInputs {
			// Expected format: rootCID:subrootCID1,subrootCID2,...
			parts := strings.SplitN(rootInput, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid root input format: %s (%d)", rootInput, len(parts))
			}
			rootCID := parts[0]
			subrootsStr := parts[1]
			subrootCIDStrs := strings.Split(subrootsStr, "+")

			if rootCID == "" || len(subrootCIDStrs) == 0 {
				return fmt.Errorf("rootCID and at least one subrootCID are required")
			}

			var subroots []SubrootEntry
			for _, subrootCID := range subrootCIDStrs {
				subroots = append(subroots, SubrootEntry{SubrootCID: subrootCID})
			}

			addRootRequests = append(addRootRequests, AddRootRequest{
				RootCID:  rootCID,
				Subroots: subroots,
			})
		}

		// Construct the request payload
		requestBodyBytes, err := json.Marshal(addRootRequests)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Construct the POST URL
		postURL := fmt.Sprintf("%s/pdp/proof-sets/%d/roots", serviceURL, proofSetID)

		// Create the POST request
		req, err := http.NewRequest("POST", postURL, bytes.NewBuffer(requestBodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", "application/json")

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Read and display the response
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %v", err)
		}
		bodyString := string(bodyBytes)

		if resp.StatusCode == http.StatusCreated {
			fmt.Printf("Roots added to proof set ID %d successfully.\n", proofSetID)
			fmt.Printf("Response: %s\n", bodyString)
		} else {
			return fmt.Errorf("failed to add roots, status code %d: %s", resp.StatusCode, bodyString)
		}

		return nil
	},
}
