package main

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/golang-jwt/jwt/v4"
	"github.com/ipfs/go-cid"
	"github.com/minio/sha256-simd"
	"github.com/schollz/progressbar/v3"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-commp-utils/nonffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	curiobuild "github.com/filecoin-project/curio/build"
	curioproof "github.com/filecoin-project/curio/lib/proof"
)

// validateExtraData checks if the provided hex string is valid and within the size limit.
func validateExtraData(extraDataHexStr string) error {
	if extraDataHexStr == "" {
		return nil // No data to validate
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(extraDataHexStr, "0x"))
	if err != nil {
		return fmt.Errorf("failed to decode hex in extra-data: %w", err)
	}
	if len(decoded) > 2048 {
		return fmt.Errorf("decoded extra-data exceeds maximum size of 2048 bytes (decoded length: %d)", len(decoded))
	}
	return nil
}

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
			uploadFileCmd,   // upload a file to a pdp service in many chunks
			downloadFileCmd, // download a file from curio

			createDataSetCmd,    // create a new data set on the PDP service
			getDataSetStatusCmd, // get the status of a data set creation on the PDP service
			getDataSetCmd,       // retrieve the details of a data set from the PDP service

			addPiecesCmd,
			removePiecesCmd, // schedule pieces for removal after next proof submission
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
		privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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

		file, err := os.OpenFile("pdpservice.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
		jwtToken, err := getJWTTokenForService(serviceName)
		if err != nil {
			return err
		}

		// Append /pdp/ping to the service URL
		pingURL := serviceURL + "/pdp/ping"

		// Create the GET request
		req, err := http.NewRequest("GET", pingURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		if jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
		}

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

func getJWTTokenForService(serviceName string) (string, error) {
	if serviceName == "public" {
		return "", nil // No JWT needed for public service
	}

	privKey, err := loadPrivateKey()
	if err != nil {
		return "", err
	}

	return createJWTToken(serviceName, privKey)
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

func preparePiece(r io.ReadSeeker) (cid.Cid, uint64, []byte, []byte, error) {
	// Create commp calculator
	cp := &commp.Calc{}

	// Copy data into commp calculator
	_, err := io.Copy(cp, r)
	if err != nil {
		return cid.Undef, 0, nil, nil, fmt.Errorf("failed to read input file: %v", err)
	}

	// Finalize digest
	digest, paddedPieceSize, err := cp.Digest()
	if err != nil {
		return cid.Undef, 0, nil, nil, fmt.Errorf("failed to compute digest: %v", err)
	}

	// Convert digest to CID
	pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		return cid.Undef, 0, nil, nil, fmt.Errorf("failed to compute piece CID: %v", err)
	}

	// now compute sha256
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return cid.Undef, 0, nil, nil, fmt.Errorf("failed to seek file: %v", err)
	}

	h := sha256.New()
	_, err = io.Copy(h, r)
	if err != nil {
		return cid.Undef, 0, nil, nil, fmt.Errorf("failed to read input file: %v", err)
	}

	// Finalize digest
	shadigest := h.Sum(nil)
	return pieceCIDComputed, paddedPieceSize, digest, shadigest, nil
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
		pieceCIDComputed, paddedPieceSize, _, shadigest, err := preparePiece(file)
		if err != nil {
			return fmt.Errorf("failed to prepare piece: %v", err)
		}

		// Output the piece CID and size
		fmt.Printf("Piece CID: %s\n", pieceCIDComputed)
		fmt.Printf("SHA256: %x\n", shadigest)
		fmt.Printf("Padded Piece Size: %d bytes\n", paddedPieceSize)
		fmt.Printf("Raw Piece Size: %d bytes\n", pieceSize)

		return nil
	},
}

func startLocalNotifyServer() (string, chan struct{}, error) {
	var notifyReceived chan struct{}
	var server *http.Server
	var ln net.Listener

	notifyReceived = make(chan struct{})
	var err error
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("failed to start local HTTP server: %v", err)
	}
	serverAddr := fmt.Sprintf("http://%s/notify", ln.Addr().String())

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
	return serverAddr, notifyReceived, nil
}

func uploadOnePiece(client *http.Client, serviceURL string, reqBody []byte, jwtToken string, r io.ReadSeeker, pieceSize int64, localNotifWait bool, notifyReceived chan struct{}, verbose bool) error {
	req, err := http.NewRequest("POST", serviceURL+"/pdp/piece", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	if jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+jwtToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if verbose {
			fmt.Println("http.StatusOK")
		}
		// Piece already exists, get the pieceCID from the response
		var respData map[string]string
		err = json.NewDecoder(resp.Body).Decode(&respData)
		if err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		pieceCID := respData["pieceCID"]
		if verbose {
			fmt.Printf("Piece already exists on the server. Piece CID: %s\n", pieceCID)
		}
		return nil
	} else if resp.StatusCode == http.StatusCreated {
		if verbose {
			fmt.Println("http.StatusCreated")
		}
		// Get the upload URL from the Location header
		uploadURL := resp.Header.Get("Location")
		if uploadURL == "" {
			return fmt.Errorf("server did not provide upload URL in Location header")
		}

		// Upload the piece data via PUT
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek file: %v", err)
		}
		uploadReq, err := http.NewRequest("PUT", serviceURL+uploadURL, r)
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
		if localNotifWait {
			if verbose {
				fmt.Println("Waiting for server notification...")
			}
			<-notifyReceived
		}

		return nil
	} else {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status code %d: %s", resp.StatusCode, string(body))
	}
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
			var err error
			jwtToken, err = getJWTTokenForService(serviceName)
			if err != nil {
				return err
			}
		}

		if hashType != "sha256" && hashType != "commp" {
			return fmt.Errorf("invalid hash type: %s", hashType)
		}

		if localNotifWait && notifyURL != "" {
			return fmt.Errorf("cannot specify both --notify-url and --local-notif-wait")
		}

		var notifyReceived chan struct{}
		var err error

		if localNotifWait {
			notifyURL, notifyReceived, err = startLocalNotifyServer()
			if err != nil {
				return fmt.Errorf("failed to start local HTTP server: %v", err)
			}
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
		_, _, commpDigest, shadigest, err := preparePiece(file)
		if err != nil {
			return fmt.Errorf("failed to prepare piece: %v", err)
		}

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
		if err := uploadOnePiece(client, serviceURL, reqBody, jwtToken, file, pieceSize, localNotifWait, notifyReceived, true); err != nil {
			return fmt.Errorf("failed to upload piece: %v", err)
		}

		fmt.Println("Piece uploaded successfully.")
		return nil
	},
}

var uploadFileCmd = &cli.Command{
	Name:      "upload-file",
	Usage:     "Upload a file to a PDP Service in many chunks",
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
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "Verbose output",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Calculate chunks but don't upload",
			Value: false,
		},
		&cli.StringFlag{
			Name:  "chunk-file",
			Usage: "Output file to write chunks to",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		inputFile := cctx.Args().Get(0)
		if inputFile == "" {
			return fmt.Errorf("input file is required")
		}

		serviceURL := cctx.String("service-url")
		jwtToken := cctx.String("jwt-token")
		serviceName := cctx.String("service-name")
		hashType := cctx.String("hash-type")
		localNotifWait := cctx.Bool("local-notif-wait")
		notifyURL := cctx.String("notify-url")
		verbose := cctx.Bool("verbose")
		dryRun := cctx.Bool("dry-run")
		chunkFileName := cctx.String("chunk-file")
		var chunkFile *os.File
		if chunkFileName != "" {
			var err error
			chunkFile, err = os.Create(chunkFileName)
			if err != nil {
				return fmt.Errorf("failed to create chunk file: %v", err)
			}
			defer chunkFile.Close()
		}
		if jwtToken == "" {
			if serviceName == "" {
				return fmt.Errorf("either --jwt-token or --service-name must be provided")
			}
			var err error
			jwtToken, err = getJWTTokenForService(serviceName)
			if err != nil {
				return err
			}
		}

		// Open input file
		file, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open input file: %v", err)
		}
		defer file.Close()

		// Get the file size
		fi, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat input file: %v", err)
		}
		fileSize := fi.Size()
		// Make padded chunk size as big as allowed
		paddedChunkSize := curioproof.MaxMemtreeSize
		chunkSize := int64((paddedChunkSize * 127) / 128) // make room for padding

		// Progress bar
		bar := progressbar.NewOptions(1, progressbar.OptionSetDescription("Uploading..."))
		if int(fileSize/chunkSize) > 0 {
			bar = progressbar.NewOptions(int(fileSize/chunkSize), progressbar.OptionSetDescription("Uploading..."))
		}

		// Setup local server if needed
		var notifyReceived chan struct{}
		if localNotifWait {
			notifyURL, notifyReceived, err = startLocalNotifyServer()
			if err != nil {
				return fmt.Errorf("failed to start local HTTP server: %v", err)
			}
		}

		// group piece aggregations for tracking as onchain pieces into sector size chunks
		type pieceSetInfo struct {
			pieces      []abi.PieceInfo
			subPieceStr string
		}
		pieceSets := []pieceSetInfo{}
		pieceSets = append(pieceSets, pieceSetInfo{
			pieces:      make([]abi.PieceInfo, 0),
			subPieceStr: "",
		})
		pieceSize := uint64(0)
		maxRootSize, err := abi.RegisteredSealProof_StackedDrg64GiBV1_1.SectorSize()
		if err != nil {
			return fmt.Errorf("failed to get sector size: %v", err)
		}
		counter := 0
		client := &http.Client{}
		for idx := int64(0); idx < fileSize; idx += chunkSize {
			// Read the chunk
			buf := make([]byte, chunkSize)
			n, err := file.ReadAt(buf, idx)
			if err != nil && err != io.EOF {
				return fmt.Errorf("failed to read file chunk: %v", err)
			}
			// Prepare the piece
			chunkReader := bytes.NewReader(buf[:n])
			commP, paddedPieceSize, commpDigest, shadigest, err := preparePiece(chunkReader)
			if err != nil {
				return fmt.Errorf("failed to prepare piece: %v", err)
			}
			if !dryRun {
				// Prepare the request data
				var checkData map[string]interface{}
				switch hashType {
				case "sha256":
					checkData = map[string]interface{}{
						"name": "sha2-256",
						"hash": hex.EncodeToString(shadigest),
						"size": n,
					}
				case "commp":
					checkData = map[string]interface{}{
						"name": "sha2-256-trunc254-padded",
						"hash": hex.EncodeToString(commpDigest),
						"size": n,
					}
				default:
					return fmt.Errorf("unsupported hash type: %s", hashType)
				}

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

				// Upload the piece
				err = uploadOnePiece(client, serviceURL, reqBody, jwtToken, chunkReader, int64(n), localNotifWait, notifyReceived, verbose)
				if err != nil {
					return fmt.Errorf("failed to upload piece: %v", err)
				}
			}
			if chunkFile != nil {
				if _, err := chunkFile.Write([]byte(fmt.Sprintf("%s\n", commP))); err != nil {
					return fmt.Errorf("failed to write chunk to file: %v", err)
				}
			}
			if pieceSize+paddedPieceSize > uint64(maxRootSize) {
				pieceSets = append(pieceSets, pieceSetInfo{
					pieces:      make([]abi.PieceInfo, 0),
					subPieceStr: "",
				})
				pieceSize = 0
			}
			pieceSize += paddedPieceSize
			pieceSets[len(pieceSets)-1].pieces = append(pieceSets[len(pieceSets)-1].pieces, abi.PieceInfo{Size: abi.PaddedPieceSize(paddedPieceSize), PieceCID: commP})
			pieceSets[len(pieceSets)-1].subPieceStr = fmt.Sprintf("%s+%s", pieceSets[len(pieceSets)-1].subPieceStr, commP)
			counter++
			if err := bar.Set(int(counter)); err != nil {
				return fmt.Errorf("failed to update progress bar: %v", err)
			}
		}

		for i, pieceSet := range pieceSets {
			pieceSize := uint64(0)
			for _, piece := range pieceSet.pieces {
				pieceSize += uint64(piece.Size)
			}
			fmt.Printf("%d: pieceSize: %d\n", i, pieceSize)
			pieceCid, err := nonffi.GenerateUnsealedCID(abi.RegisteredSealProof_StackedDrg64GiBV1_1, pieceSet.pieces)
			if err != nil {
				return fmt.Errorf("failed to generate unsealed CID: %v", err)
			}
			s := fmt.Sprintf("%s:%s\n", pieceCid, pieceSet.subPieceStr[1:])
			fmt.Printf("%s\n", s)
		}

		return nil
	},
}

var createDataSetCmd = &cli.Command{
	Name:  "create-data-set",
	Usage: "Create a new data set on the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "pdp-service-contract",
			Usage:    "Address of the pdp service contract",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "extra-data",
			Usage:    "Optional ABI-encoded extra data as a hex string to pass to the listener contract (max 2048 bytes)",
			Required: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")
		recordKeeper := cctx.String("pdp-service-contract")
		extraDataHexStr := cctx.String("extra-data")

		// Validate extraData hex string and its decoded length
		if err := validateExtraData(extraDataHexStr); err != nil {
			return err
		}

		// Create the JWT token
		jwtToken, err := getJWTTokenForService(serviceName)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Construct the request payload
		requestBody := map[string]string{
			"recordKeeper": recordKeeper,
		}
		if extraDataHexStr != "" {
			requestBody["extraData"] = extraDataHexStr
		}

		requestBodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Append /pdp/data-sets to the service URL
		postURL := serviceURL + "/pdp/data-sets"

		// Create the POST request
		req, err := http.NewRequest("POST", postURL, bytes.NewBuffer(requestBodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		if jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
		}
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
			fmt.Printf("Data set creation initiated successfully.\n")
			fmt.Printf("Location: %s\n", location)
			fmt.Printf("Response: %s\n", bodyString)
		} else {
			return fmt.Errorf("failed to create data set, status code %d: %s", resp.StatusCode, bodyString)
		}

		return nil
	},
}

var getDataSetStatusCmd = &cli.Command{
	Name:  "get-data-set-create-status",
	Usage: "Get the status of a data set creation on the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "tx-hash",
			Usage:    "Transaction hash of the data set creation",
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

		// Create the JWT token
		jwtToken, err := getJWTTokenForService(serviceName)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Ensure txHash starts with '0x'
		if !strings.HasPrefix(txHash, "0x") {
			txHash = "0x" + txHash
		}
		txHash = strings.ToLower(txHash) // Ensure txHash is in lowercase

		// Construct the request URL
		getURL := fmt.Sprintf("%s/pdp/data-sets/created/%s", serviceURL, txHash)

		// Create the GET request
		req, err := http.NewRequest("GET", getURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		if jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
		}

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
				DataSetCreated    bool    `json:"dataSetCreated"`
				Service           string  `json:"service"`
				TxStatus          string  `json:"txStatus"`
				OK                *bool   `json:"ok"`
				DataSetId         *uint64 `json:"dataSetId,omitempty"`
			}
			err = json.Unmarshal(bodyBytes, &response)
			if err != nil {
				return fmt.Errorf("failed to parse JSON response: %v", err)
			}

			// Display the status
			fmt.Printf("Data Set Creation Status:\n")
			fmt.Printf("Transaction Hash: %s\n", response.CreateMessageHash)
			fmt.Printf("Transaction Status: %s\n", response.TxStatus)
			if response.OK != nil {
				fmt.Printf("Transaction Successful: %v\n", *response.OK)
			} else {
				fmt.Printf("Transaction Successful: Pending\n")
			}
			fmt.Printf("Dataset Created: %v\n", response.DataSetCreated)
			if response.DataSetId != nil {
				fmt.Printf("Dataset ID: %d\n", *response.DataSetId)
			}
		} else {
			return fmt.Errorf("failed to get data set status, status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		return nil
	},
}

var getDataSetCmd = &cli.Command{
	Name:      "get-data-set",
	Usage:     "Retrieve the details of a data set from the PDP service",
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

		// Create the JWT token
		jwtToken, err := getJWTTokenForService(serviceName)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Construct the request URL
		getURL := fmt.Sprintf("%s/pdp/data-sets/%d", serviceURL, setID)

		// Create the GET request
		req, err := http.NewRequest("GET", getURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		if jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
		}

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
				Pieces             []struct {
					PieceId        uint64 `json:"pieceId"`
					PieceCid       string `json:"pieceCid"`
					SubPieceCid    string `json:"subPieceCid"`
					SubPieceOffset int64  `json:"subPieceOffset"`
				} `json:"roots"`
			}
			err = json.Unmarshal(bodyBytes, &response)
			if err != nil {
				return fmt.Errorf("failed to parse JSON response: %v", err)
			}

			// Display the data set details
			fmt.Printf("Data Set ID: %d\n", response.ID)
			fmt.Printf("Next Challenge Epoch: %d\n", response.NextChallengeEpoch)
			fmt.Printf("Pieces:\n")
			for _, root := range response.Pieces {
				fmt.Printf("  - Root ID: %d\n", root.PieceId)
				fmt.Printf("    Root CID: %s\n", root.PieceCid)
				fmt.Printf("    SubPiece CID: %s\n", root.SubPieceCid)
				fmt.Printf("    SubPiece Offset: %d\n", root.SubPieceOffset)
				fmt.Println()
			}
		} else {
			return fmt.Errorf("failed to get data set, status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		return nil
	},
}

var addPiecesCmd = &cli.Command{
	Name:  "add-pieces",
	Usage: "Add pieces to a data set on the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:     "data-set-id",
			Usage:    "ID of the data set to which pieces will be added",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:     "piece",
			Usage:    "Piece CID and its subPieces. Format: pieceCID:subPieceCID1+subPieceCID2,...",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "extra-data",
			Usage:    "Optional ABI-encoded extra data as a hex string to pass to the listener contract (max 2048 bytes)",
			Required: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")
		dataSetId := cctx.Uint64("data-set-id")
		pieceInputs := cctx.StringSlice("piece")
		extraDataHexStr := cctx.String("extra-data")

		// Validate extraData hex string and its decoded length
		if err := validateExtraData(extraDataHexStr); err != nil {
			return err
		}

		// Create the JWT token
		jwtToken, err := getJWTTokenForService(serviceName)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Parse the piece inputs to construct the request payload
		type SubpieceEntry struct {
			SubpieceCID string `json:"subPieceCid"`
		}

		type AddPieceRequest struct {
			PieceCID  string          `json:"pieceCid"`
			Subpieces []SubpieceEntry `json:"subPieces"`
		}

		var addPieceRequests []AddPieceRequest

		for _, pieceInput := range pieceInputs {
			// Expected format: pieceCID:subPieceCID1,subPieceCID2,...
			parts := strings.SplitN(pieceInput, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid piece input format: %s (%d)", pieceInput, len(parts))
			}
			pieceCID := parts[0]
			subPiecesStr := parts[1]
			subPieceCIDStrs := strings.Split(subPiecesStr, "+")

			if pieceCID == "" || len(subPieceCIDStrs) == 0 {
				return fmt.Errorf("pieceCID and at least one subPieceCID are required")
			}

			var subPieces []SubpieceEntry
			for _, subPieceCID := range subPieceCIDStrs {
				subPieces = append(subPieces, SubpieceEntry{SubpieceCID: subPieceCID})
			}

			addPieceRequests = append(addPieceRequests, AddPieceRequest{
				PieceCID:  pieceCID,
				Subpieces: subPieces,
			})
		}

		// Construct the full request payload including extraData
		type AddPiecesPayload struct {
			Pieces    []AddPieceRequest `json:"pieces"`
			ExtraData *string           `json:"extraData,omitempty"`
		}

		payload := AddPiecesPayload{
			Pieces: addPieceRequests,
		}
		if extraDataHexStr != "" {
			// Pass the validated 0x-prefixed hex string directly
			payload.ExtraData = &extraDataHexStr
		}

		requestBodyBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %v", err)
		}

		// Construct the POST URL
		postURL := fmt.Sprintf("%s/pdp/data-sets/%d/pieces", serviceURL, dataSetId)

		// Create the POST request
		req, err := http.NewRequest("POST", postURL, bytes.NewBuffer(requestBodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		if jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
		}
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
			fmt.Printf("Pieces added to data set ID %d successfully.\n", dataSetId)
			fmt.Printf("Response: %s\n", bodyString)
		} else {
			return fmt.Errorf("failed to add pieces, status code %d: %s", resp.StatusCode, bodyString)
		}

		return nil
	},
}

var downloadFileCmd = &cli.Command{
	Name:  "download-file",
	Usage: "Download a file from the PDP service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "chunk-file",
			Usage:    "File to read ordered chunk list for retrieving",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "output-file",
			Usage:    "File to write downloaded data to",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		chunkFileName := cctx.String("chunk-file")
		outputFileName := cctx.String("output-file")

		// Open the chunk file for reading
		chunkFile, err := os.Open(chunkFileName)
		if err != nil {
			return fmt.Errorf("failed to open chunk file: %v", err)
		}
		defer chunkFile.Close()

		// Open the output file for writing
		outputFile, err := os.Create(outputFileName)
		if err != nil {
			return fmt.Errorf("failed to create output file: %v", err)
		}
		defer outputFile.Close()

		// Read all CIDs from the chunk file
		var cids []string
		scanner := bufio.NewScanner(chunkFile)
		for scanner.Scan() {
			cid := strings.TrimSpace(scanner.Text())
			if cid != "" {
				cids = append(cids, cid)
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading chunk file: %v", err)
		}

		// Create HTTP client
		client := &http.Client{}

		// Create progress bar
		bar := progressbar.NewOptions(len(cids),
			progressbar.OptionSetDescription("Downloading..."),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount())

		// Download each piece and write it to the output file
		for i, cidString := range cids {
			// Create the download URL
			downloadURL := fmt.Sprintf("%s/piece/%s", serviceURL, cidString)

			// Create the GET request
			req, err := http.NewRequest("GET", downloadURL, nil)
			if err != nil {
				return fmt.Errorf("failed to create request for CID %s: %v", cidString, err)
			}

			// Send the request
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to download piece %s: %v", cidString, err)
			}

			// Check response status
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return fmt.Errorf("failed to download piece %s: status code %d", cidString, resp.StatusCode)
			}

			// Stream the response body to the output file
			_, err = io.Copy(outputFile, resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("failed to write piece %s to output file: %v", cidString, err)
			}

			// Update progress bar
			if err := bar.Set(i + 1); err != nil {
				return fmt.Errorf("failed to update progress bar: %v", err)
			}
		}

		fmt.Printf("\nDownload completed successfully. Saved to %s\n", outputFileName)
		return nil
	},
}

var removePiecesCmd = &cli.Command{
	Name:  "remove-pieces",
	Usage: "Schedule pieces for removal after next proof submission",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    "URL of the PDP service",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:     "data-set-id",
			Usage:    "ID of the data set from which pieces will be removed",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "service-name",
			Usage:    "Service Name to include in the JWT token",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:     "piece-id",
			Usage:    "Piece ID for removal",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		serviceURL := cctx.String("service-url")
		serviceName := cctx.String("service-name")
		dataSetId := cctx.Uint64("data-set-id")
		pieceId := cctx.Uint64("piece-id")

		// Create the JWT token
		jwtToken, err := getJWTTokenForService(serviceName)
		if err != nil {
			return fmt.Errorf("failed to create JWT token: %v", err)
		}

		// Construct the POST URL
		deleteURL := fmt.Sprintf("%s/pdp/data-sets/%d/pieces/%d", serviceURL, dataSetId, pieceId)
		fmt.Printf("Delete URL: %s\n", deleteURL)

		// Create the POST request
		req, err := http.NewRequest("DELETE", deleteURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %v", err)
		}
		if jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
		}
		req.Header.Set("Content-Type", "application/json")

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %v", err)
		}

		// Read and display the response
		if resp.StatusCode == http.StatusNoContent {
			fmt.Printf("Piece %d scheduled for removal from data set ID %d.\n", pieceId, dataSetId)
		} else {
			return fmt.Errorf("failed to remove piece, status code %d", resp.StatusCode)
		}

		return nil
	},
}
