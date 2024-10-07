package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/golang-jwt/jwt/v4"
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

			createProofSetCmd, // create a new proof set on the PDP service
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

		// Output the piece CID and size
		fmt.Printf("Piece CID: %s\n", pieceCIDComputed)
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

		// First, compute the PieceCID
		// Open input file
		file, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("failed to open input file: %v", err)
		}
		defer file.Close()

		cp := &commp.Calc{}

		// Copy data into commp calculator
		_, err = io.Copy(cp, file)
		if err != nil {
			return fmt.Errorf("failed to read input file: %v", err)
		}

		// Finalize digest
		digest, _, err := cp.Digest()
		if err != nil {
			return fmt.Errorf("failed to compute digest: %v", err)
		}

		// Convert digest to CID
		pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
		if err != nil {
			return fmt.Errorf("failed to compute piece CID: %v", err)
		}

		// Send POST /pdp/piece to the PDP service
		reqData := map[string]interface{}{
			"pieceCid": pieceCIDComputed.String(),
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

		if resp.StatusCode == http.StatusNoContent {
			fmt.Println("Piece already exists on the server.")
			return nil
		} else if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server returned status code %d: %s", resp.StatusCode, string(body))
		}

		// Get the upload URL from the Location header
		uploadURL := resp.Header.Get("Location")
		if uploadURL == "" {
			return fmt.Errorf("server did not provide upload URL in Location header")
		}

		// Upload the piece data via PUT
		_, err = file.Seek(0, io.SeekStart) // Reset file pointer to the beginning
		if err != nil {
			return fmt.Errorf("failed to seek file: %v", err)
		}
		uploadReq, err := http.NewRequest("PUT", serviceURL+uploadURL, file)
		if err != nil {
			return fmt.Errorf("failed to create upload request: %v", err)
		}

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

		return nil
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
