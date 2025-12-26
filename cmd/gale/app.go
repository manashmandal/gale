package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/manashmandal/gale/internal/config"
	"github.com/manashmandal/gale/internal/github"
	"github.com/spf13/cobra"
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Manage GitHub App configuration",
	Long: `Manage GitHub App authentication for Gale.

GitHub App provides several advantages over PAT:
  - Higher rate limits (5000/hr per installation vs shared)
  - Automatic token rotation (1hr tokens)
  - Granular permissions
  - Centralized webhook configuration

Subcommands:
  setup    - Show manual setup instructions
  validate - Test app configuration
  create   - Auto-create GitHub App (requires PAT)`,
}

var appSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Show GitHub App setup instructions",
	Long:  `Display step-by-step instructions for creating and configuring a private GitHub App.`,
	Run:   runAppSetup,
}

var appValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate GitHub App configuration",
	Long:  `Test that your GitHub App configuration is valid and can authenticate.`,
	RunE:  runAppValidate,
}

var appCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a GitHub App interactively",
	Long: `Create a new private GitHub App interactively.

This command will guide you through creating a GitHub App with the
correct permissions for Gale. You'll need to complete some steps
manually in your browser.`,
	RunE: runAppCreate,
}

func init() {
	rootCmd.AddCommand(appCmd)
	appCmd.AddCommand(appSetupCmd)
	appCmd.AddCommand(appValidateCmd)
	appCmd.AddCommand(appCreateCmd)
}

func runAppSetup(cmd *cobra.Command, args []string) {
	fmt.Println(`
GitHub App Setup Instructions
==============================

GitHub Apps provide better security and higher rate limits than PATs.
Follow these steps to create a private GitHub App for Gale:

STEP 1: Create the GitHub App
-----------------------------
1. Go to: https://github.com/settings/apps/new
   (For org: https://github.com/organizations/YOUR_ORG/settings/apps/new)

2. Fill in the form:
   - GitHub App name: gale-runner (must be unique)
   - Homepage URL: https://github.com/manashmandal/gale
   - Webhook URL: https://YOUR_SERVER:8080/webhook
   - Webhook secret: Generate a random string (save this!)

3. Set Permissions:
   Repository permissions:
   - Actions: Read-only
   - Metadata: Read-only

4. Subscribe to events:
   - Workflow jobs (check this box)

5. Where can this app be installed?
   - Only on this account (for private app)

6. Click "Create GitHub App"

STEP 2: Get Credentials
-----------------------
After creating the app:

1. Note the App ID (shown on the app page)

2. Generate a private key:
   - Scroll down to "Private keys"
   - Click "Generate a private key"
   - A .pem file will download - keep it safe!

STEP 3: Install the App
-----------------------
1. On your app page, click "Install App" (left sidebar)
2. Select your account/organization
3. Choose repositories:
   - All repositories, OR
   - Select specific repositories
4. Click "Install"

STEP 4: Configure Gale
----------------------
Update your config.yaml:

` + "```yaml" + `
github:
  app:
    app_id: YOUR_APP_ID
    private_key_path: /path/to/downloaded-key.pem
    webhook_secret: ${GALE_WEBHOOK_SECRET}

webhook:
  port: 8080

runner:
  image: myoung34/github-runner:latest
  labels:
    - gale
    - self-hosted
` + "```" + `

Or use environment variable for the private key:

` + "```bash" + `
export GALE_PRIVATE_KEY="$(cat /path/to/key.pem)"
export GALE_WEBHOOK_SECRET="your-webhook-secret"
` + "```" + `

STEP 5: Start Gale
------------------
` + "```bash" + `
gale webhook
` + "```" + `

Validate your setup:
` + "```bash" + `
gale app validate
` + "```" + `

Need help? Visit: https://github.com/manashmandal/gale`)
}

func runAppValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	fmt.Println("Validating GitHub App configuration...")
	fmt.Println()

	// Check if app mode is configured
	if !cfg.IsAppMode() {
		fmt.Println("❌ Not configured for GitHub App mode")
		fmt.Println()
		fmt.Println("Your config is using PAT authentication.")
		fmt.Println("To use GitHub App, add the following to your config.yaml:")
		fmt.Println()
		fmt.Println("  github:")
		fmt.Println("    app:")
		fmt.Println("      app_id: YOUR_APP_ID")
		fmt.Println("      private_key_path: /path/to/key.pem")
		fmt.Println("      webhook_secret: ${GALE_WEBHOOK_SECRET}")
		fmt.Println()
		fmt.Println("Run 'gale app setup' for detailed instructions.")
		return nil
	}

	fmt.Printf("✓ App ID: %d\n", cfg.GitHub.App.AppID)

	// Check private key
	privateKey, err := cfg.GetPrivateKey()
	if err != nil {
		fmt.Printf("❌ Private key: %v\n", err)
		return fmt.Errorf("private key not configured correctly")
	}
	fmt.Println("✓ Private key: Found")

	// Try to create AppClient
	appClient, err := github.NewAppClient(cfg.GitHub.App.AppID, privateKey)
	if err != nil {
		fmt.Printf("❌ Client creation: %v\n", err)
		return fmt.Errorf("failed to create app client")
	}
	fmt.Println("✓ Client: Created successfully")

	// Try to generate JWT
	jwt, err := appClient.GenerateJWT()
	if err != nil {
		fmt.Printf("❌ JWT generation: %v\n", err)
		return fmt.Errorf("failed to generate JWT")
	}
	fmt.Printf("✓ JWT: Generated (%d chars)\n", len(jwt))

	// Validate credentials with GitHub API
	fmt.Println()
	fmt.Println("Testing connection to GitHub API...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := appClient.ValidateCredentials(ctx); err != nil {
		fmt.Printf("❌ API validation: %v\n", err)
		return fmt.Errorf("credentials validation failed")
	}
	fmt.Println("✓ API: Credentials validated")

	// Get app info
	appInfo, err := appClient.GetAppInfo(ctx)
	if err != nil {
		fmt.Printf("⚠ Could not get app info: %v\n", err)
	} else {
		fmt.Printf("✓ App name: %s\n", appInfo.GetName())
		fmt.Printf("✓ App slug: %s\n", appInfo.GetSlug())
	}

	// Get installations
	installations, err := appClient.GetInstallations(ctx)
	if err != nil {
		fmt.Printf("⚠ Could not list installations: %v\n", err)
	} else {
		fmt.Printf("✓ Installations: %d\n", len(installations))
		for _, inst := range installations {
			account := inst.GetAccount()
			fmt.Printf("  - %s (ID: %d)\n", account.GetLogin(), inst.GetID())
		}
	}

	// Check webhook secret
	fmt.Println()
	if cfg.GetWebhookSecret() != "" {
		fmt.Println("✓ Webhook secret: Configured")
	} else {
		fmt.Println("⚠ Webhook secret: Not configured (signature verification disabled)")
	}

	fmt.Println()
	fmt.Println("✅ GitHub App configuration is valid!")
	fmt.Println()
	fmt.Println("Start the webhook server with:")
	fmt.Println("  gale webhook")

	return nil
}

func runAppCreate(cmd *cobra.Command, args []string) error {
	fmt.Println("GitHub App Creation Wizard")
	fmt.Println("==========================")
	fmt.Println()
	fmt.Println("This wizard will help you create a GitHub App for Gale.")
	fmt.Println("Since GitHub Apps require browser interaction, we'll guide you")
	fmt.Println("through the process step by step.")
	fmt.Println()

	// Get organization or user
	var accountType string
	survey.AskOne(&survey.Select{
		Message: "Where will this app be installed?",
		Options: []string{
			"Personal account",
			"Organization",
		},
	}, &accountType)

	var createURL string
	if strings.Contains(accountType, "Organization") {
		var orgName string
		survey.AskOne(&survey.Input{
			Message: "Organization name:",
		}, &orgName, survey.WithValidator(survey.Required))
		createURL = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new", orgName)
	} else {
		createURL = "https://github.com/settings/apps/new"
	}

	var appName string
	survey.AskOne(&survey.Input{
		Message: "App name (must be unique on GitHub):",
		Default: "gale-runner",
	}, &appName, survey.WithValidator(survey.Required))

	var webhookURL string
	survey.AskOne(&survey.Input{
		Message: "Webhook URL (where Gale will receive events):",
		Help:    "Example: https://your-server.com:8080/webhook",
	}, &webhookURL, survey.WithValidator(survey.Required))

	// Generate webhook secret
	webhookSecret := generateRandomString(32)

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("STEP 1: Create the GitHub App")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Open this URL in your browser:")
	fmt.Printf("  %s\n", createURL)
	fmt.Println()
	fmt.Println("Fill in the form with these values:")
	fmt.Println()
	fmt.Printf("  GitHub App name:    %s\n", appName)
	fmt.Printf("  Homepage URL:       https://github.com/manashmandal/gale\n")
	fmt.Printf("  Webhook URL:        %s\n", webhookURL)
	fmt.Printf("  Webhook secret:     %s\n", webhookSecret)
	fmt.Println()
	fmt.Println("Permissions (Repository):")
	fmt.Println("  - Actions:  Read-only")
	fmt.Println("  - Metadata: Read-only")
	fmt.Println()
	fmt.Println("Subscribe to events:")
	fmt.Println("  - [x] Workflow jobs")
	fmt.Println()
	fmt.Println("Where can this app be installed:")
	fmt.Println("  - (*) Only on this account")
	fmt.Println()
	fmt.Println("Then click 'Create GitHub App'")
	fmt.Println()

	var created bool
	survey.AskOne(&survey.Confirm{
		Message: "Have you created the GitHub App?",
		Default: false,
	}, &created)

	if !created {
		fmt.Println("\nCome back when you've created the app!")
		return nil
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("STEP 2: Get the App ID and Private Key")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("On your new app's page:")
	fmt.Println("1. Note the 'App ID' number near the top")
	fmt.Println("2. Scroll down to 'Private keys'")
	fmt.Println("3. Click 'Generate a private key'")
	fmt.Println("4. A .pem file will download - save it securely!")
	fmt.Println()

	var appID int64
	survey.AskOne(&survey.Input{
		Message: "Enter the App ID:",
	}, &appID, survey.WithValidator(survey.Required))

	var privateKeyPath string
	survey.AskOne(&survey.Input{
		Message: "Path to the downloaded private key (.pem file):",
	}, &privateKeyPath, survey.WithValidator(survey.Required))

	// Verify the private key exists and is valid
	privateKeyPath = os.ExpandEnv(privateKeyPath)
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("could not read private key file: %w", err)
	}

	// Test that we can parse it
	_, err = github.NewAppClient(appID, keyData)
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Private key is valid!")
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("STEP 3: Install the App")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("On your app's page, click 'Install App' in the left sidebar,")
	fmt.Println("then select which repositories should trigger Gale runners.")
	fmt.Println()

	var installed bool
	survey.AskOne(&survey.Confirm{
		Message: "Have you installed the app on your repositories?",
		Default: false,
	}, &installed)

	if !installed {
		fmt.Println("\nMake sure to install the app before starting Gale!")
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("STEP 4: Save Configuration")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	var storageChoice string
	survey.AskOne(&survey.Select{
		Message: "How should the webhook secret be stored?",
		Options: []string{
			"Environment variable (recommended)",
			"Direct in config file",
		},
		Default: "Environment variable (recommended)",
	}, &storageChoice)

	useEnvVar := strings.Contains(storageChoice, "Environment")

	fmt.Println()
	fmt.Println("Add this to your config.yaml:")
	fmt.Println()
	fmt.Println("github:")
	fmt.Println("  app:")
	fmt.Printf("    app_id: %d\n", appID)
	fmt.Printf("    private_key_path: %s\n", privateKeyPath)
	if useEnvVar {
		fmt.Println("    webhook_secret: ${GALE_WEBHOOK_SECRET}")
		fmt.Println()
		fmt.Println("Set the environment variable:")
		fmt.Printf("  export GALE_WEBHOOK_SECRET=\"%s\"\n", webhookSecret)
	} else {
		fmt.Printf("    webhook_secret: %s\n", webhookSecret)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Setup Complete!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("After updating your config, run:")
	fmt.Println("  gale app validate  # Test the configuration")
	fmt.Println("  gale webhook       # Start the webhook server")
	fmt.Println()

	return nil
}

func generateRandomString(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
