package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"golang.org/x/sync/semaphore"
)

var unsupportedRuntimeError = errors.New("unsupported runtime")
var deprecatedRuntimeError = errors.New("runtime is deprecated and no longer supported")
var functionAlreadyExistsError = errors.New("function already exists, skipping creation")

type RuntimeCode struct {
	Zip     []byte
	Handler string
}

// getAvailableRuntimes fetches available runtimes for a region and returns a map of runtime -> code snippet.
func getAvailableRuntimes() map[types.Runtime]RuntimeCode {
	//jar, err := os.ReadFile("java/target/aws-sdk-version-1.0-SNAPSHOT.jar")
	//if err != nil {
	//	log.Fatalf("Unable to read JAR file: %v", err)
	//}

	node16 := RuntimeCode{
		Zip:     createZip("index.js", `const AWS = require('aws-sdk'); exports.handler = async () => ({ version: AWS.VERSION });`),
		Handler: "index.handler",
	}
	node18plus := RuntimeCode{
		Zip:     createZip("index.mjs", `import packageJson from '@aws-sdk/client-s3/package.json' with { type: 'json' }; export const handler = async () => ({ version: packageJson.version });`),
		Handler: "index.handler",
	}
	python3 := RuntimeCode{
		Zip:     createZip("index.py", `def handler(event, context): import boto3; return {"version": boto3.__version__}`),
		Handler: "index.handler",
	}
	ruby := RuntimeCode{
		Zip: createZip("handler.rb", `require 'aws-sdk-s3'; def handler(event:, context:); { version: Aws::S3::GEM_VERSION }; end`),
		//Zip:     createZip("handler.rb", `def handler(event:, context:)\nrequire 'aws-sdk-s3'\nreturn { version: Aws::S3::GEM_VERSION }\nend`),
		Handler: "handler.handler",
	}
	//java := RuntimeCode{
	//	Zip:     jar,
	//	Handler: "sdkver.Handler::handleRequest",
	//}

	runtimes := map[types.Runtime]RuntimeCode{}
	var runtime types.Runtime
	for _, r := range runtime.Values() {
		if isNodejs(r) {
			if r == types.RuntimeNodejs16x {
				runtimes[r] = node16
			} else {
				runtimes[r] = node18plus
			}
		} else if isPython3(r) {
			runtimes[r] = python3
		} else if isJava(r) {
			// skip java as the runtime doesn't seem to bundle AWS SDK
			//runtimes[r] = java
		} else if isRuby(r) {
			runtimes[r] = ruby
		} else {
			continue // Skip unsupported runtimes
		}
	}
	return runtimes
}

func isJava(r types.Runtime) bool {
	return strings.HasPrefix(string(r), "java")
}

func isPython3(r types.Runtime) bool {
	return strings.HasPrefix(string(r), "python3")
}

func isNodejs(r types.Runtime) bool {
	return strings.HasPrefix(string(r), "nodejs")
}

func isRuby(r types.Runtime) bool {
	return strings.HasPrefix(string(r), "ruby")
}

func getAllRegions(ctx context.Context, cfg aws.Config) ([]string, error) {
	client := ec2.NewFromConfig(cfg)
	out, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, err
	}
	var regions []string
	for _, r := range out.Regions {
		regions = append(regions, *r.RegionName)
	}
	return regions, nil
}

func collectData() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	regions, err := getAllRegions(ctx, cfg)
	if err != nil {
		log.Fatalf("failed to get regions: %v", err)
	}

	runtimes := getAvailableRuntimes()
	architectures := []types.Architecture{types.ArchitectureArm64, types.ArchitectureX8664}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(20)

	resultsMap, err := readJSON("results.json")
	if err != nil {
		log.Fatalf("failed to load current results: %v", err)
	}

	var failures []error

	handleOne := func(region string, runtime types.Runtime, code RuntimeCode, arch types.Architecture) {
		sem.Acquire(ctx, 1)
		defer sem.Release(1)
		defer wg.Done()
		res := Result{Version: "", Date: time.Now()}
		version, err := handleLambda(ctx, cfg, region, runtime, code, arch)
		if err != nil {
			if errors.Is(err, unsupportedRuntimeError) || errors.Is(err, deprecatedRuntimeError) {
				res.Error = err.Error()
			} else {
				log.Printf("Error invoking Lambda in %s/%s/%s: %v\n", region, runtime, arch, err)
				mu.Lock()
				defer mu.Unlock()
				failures = append(failures, fmt.Errorf("invoking Lambda in %s/%s/%s: %w", region, runtime, arch, err))
				return
			}
		} else {
			res.Version = version
		}

		// save result, if a new version is detected
		key := Key{Region: region, Runtime: string(runtime), Architecture: string(arch)}

		mu.Lock()
		defer mu.Unlock()
		if existing, ok := resultsMap[key]; !ok || len(existing) == 0 || existing[len(existing)-1].Version != res.Version || existing[len(existing)-1].Error != res.Error {
			resultsMap[key] = append([]Result{res}, resultsMap[key]...)
		}
	}

	for _, region := range regions {
		for runtime, code := range runtimes {
			for _, arch := range architectures {
				wg.Add(1)
				go handleOne(region, runtime, code, arch)
			}
		}
	}

	wg.Wait()

	// Document any failures
	if len(failures) > 0 {
		log.Println("Failures encountered:")
		// dedup failures
		seen := make(map[string]int)
		for _, err := range failures {
			msg := err.Error()
			if _, ok := seen[msg]; !ok {
				seen[msg] = 1
			} else {
				seen[msg] = seen[msg] + 1
			}
		}

		for err, count := range seen {
			log.Println("[%d] %s", count, err)
		}
	}

	// Write JSON
	writeJSON("results.json", resultsMap)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "collect" {
		collectData()
	} else if len(os.Args) > 1 && os.Args[1] == "report" {
		resultsMap, err := readJSON("results.json")
		if err != nil {
			log.Fatalf("failed to load results: %v", err)
		}
		generateHTML(resultsMap)
	} else {
		log.Println("Usage: go run main.go collect|report")
		log.Println("  collect: Collects AWS SDK versions from Lambda functions in all regions.")
		log.Println("  report: Generates an HTML report from the collected data.")
		os.Exit(1)
	}
}

func handleLambda(ctx context.Context, cfg aws.Config, region string, runtime types.Runtime, code RuntimeCode, arch types.Architecture) (string, error) {
	log.Printf("%s: %s %s", region, runtime, arch)

	client := lambda.NewFromConfig(cfg, func(o *lambda.Options) {
		o.Region = region
	})

	fnName := strings.ReplaceAll(fmt.Sprintf("sdkver-%s-%s", runtime, arch), ".", "-")

	// Create function
	_, err := client.CreateFunction(ctx, &lambda.CreateFunctionInput{
		FunctionName:  aws.String(fnName),
		Role:          aws.String(os.Getenv("LAMBDA_ROLE_ARN")),
		Runtime:       runtime,
		Handler:       aws.String(code.Handler),
		Code:          &types.FunctionCode{ZipFile: code.Zip},
		Timeout:       aws.Int32(10),
		Architectures: []types.Architecture{arch},
	})
	if err != nil {
		var invalidParams *types.InvalidParameterValueException
		if errors.As(err, &invalidParams) {
			if strings.Contains(invalidParams.ErrorMessage(), "no longer supported") || strings.Contains(invalidParams.ErrorMessage(), "'runtime' failed to satisfy constraint") {
				return "", deprecatedRuntimeError
			}
		}

		var resourceConflict *types.ResourceConflictException
		if errors.As(err, &resourceConflict) {
			log.Printf("Function %s already exists in %s, deleting and will try again next time", fnName, region)
			client.DeleteFunction(ctx, &lambda.DeleteFunctionInput{FunctionName: aws.String(fnName)})
			return "", functionAlreadyExistsError
		}

		return "", err
	}
	if runtime != types.RuntimeJava11 {
		defer client.DeleteFunction(ctx, &lambda.DeleteFunctionInput{FunctionName: aws.String(fnName)})
	}

	// Wait for function to be active, polling every 1s up to 20s
	for i := 0; i < 20; i++ {
		out, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
			FunctionName: aws.String(fnName),
		})
		if err == nil && out.Configuration != nil && out.Configuration.State == types.StateActive {
			break
		}
		if err == nil && out.Configuration != nil && out.Configuration.State == types.StateFailed {
			return "", fmt.Errorf("lambda function creation failed: %s", aws.ToString(out.Configuration.StateReason))
		}
		if i == 19 {
			return "", fmt.Errorf("lambda function %s in %s did not become active within 20 seconds", fnName, region)
		}
		time.Sleep(1 * time.Second)
	}

	// Invoke function
	out, err := client.Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(fnName),
	})
	if err != nil {
		return "", err
	}

	if out.FunctionError != nil {
		return "", fmt.Errorf("%v: %v", *out.FunctionError, string(out.Payload))
	}

	// Parse version from response
	return parseVersion(out.Payload)
}

// createZip creates a zip archive in memory with a single file.
func createZip(filename string, content string) []byte {
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	f, err := zipWriter.Create(filename)
	if err != nil {
		panic(err)
	}
	_, err = f.Write([]byte(content))
	if err != nil {
		panic(err)
	}
	if err := zipWriter.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func parseVersion(payload []byte) (string, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return "", fmt.Errorf("failed to parse payload: %w", err)
	}
	if v, ok := resp["version"]; ok {
		if versionStr, ok := v.(string); ok {
			return versionStr, nil
		}
	}
	return "", fmt.Errorf("version not found in payload")
}

func generateHTML(results Results) {
	funcMap := template.FuncMap{
		"now": time.Now,
	}

	t, err := template.New("report").Funcs(funcMap).ParseFiles("report.gohtml")
	if err != nil {
		log.Fatalf("failed to load template: %v", err)
	}

	targetFile := "index.html"
	if len(os.Args) > 2 {
		targetFile = os.Args[2]
	}

	f, _ := os.Create(targetFile)
	defer f.Close()
	if err := t.Execute(f, results); err != nil {
		log.Fatalf("failed to execute template: %v", err)
	}
	log.Printf("HTML report generated: %s", targetFile)
}
