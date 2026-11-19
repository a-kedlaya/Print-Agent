# Palate Print Agent

Restaurant kitchen print system with multi-station order routing for Windows.

## Features

- **Multi-Station Routing**: Automatically routes orders to kitchen stations (Tandoor, Wok, Pass, etc.)
- **Backup Printers**: Automatic failover when primary printer is unavailable
- **Web-Based Order Entry**: Simple UI for entering orders with colon-separated format
- **Real-Time Dashboard**: Monitor all printers, agents, and print jobs
- **ESC/POS Support**: Works with thermal receipt printers
- **Agent-Based**: Resilient print agents that work offline and sync when reconnected

## Quick Start

### 1. Start the Complete System (One Command)

```powershell
.\START_ALL.ps1
```

This automatically:
- Starts 4 fake printers (Tandoor, Pass, Wok, Bill)
- Starts the mock Palate server
- Generates pairing codes
- Pairs and starts both agents (front-till and back-till)
- Opens the dashboard in your browser

**Access Points:**
- **Dashboard**: http://127.0.0.1:8080
- **Order Entry UI**: http://127.0.0.1:8081

### 2. Enter Orders

Open http://127.0.0.1:8081 and enter orders using this format:

```
Tandoor: 2x Paneer Tikka : 240
         1x Garlic Naan : 40

Pass:    1x Green Salad : 80
         2x Raita : 50

Wok:     1x Hakka Noodles : 180
         2x Fried Rice : 150

Order Number: 1002 / T5
```

Format: `<quantity>x <item name> : <price>`

Click "Send Order" and the system will:
- Create separate KOTs (Kitchen Order Tickets) for each station
- Print a combined customer bill
- Route each ticket to the correct printer

### 3. Stop the System

```powershell
.\STOP_ALL.ps1
```

This stops all background jobs (printers, server, agents).

## How It Works

### Order Flow

```
Order Entry UI (http://127.0.0.1:8081)
    ↓
Mock Palate Server (parses order document)
    ↓
Creates Jobs: [Tandoor KOT] [Pass KOT] [Wok KOT] [Customer Bill]
    ↓
Agents poll for jobs every few seconds
    ↓
Print Agents (front-till, back-till)
    ↓
ESC/POS Printers (network or USB)
```

### Backup Printer System

The system includes **automatic backup printer failover**:

**Configuration:**
- `p-tandoor` → backup: `p-bill`
- `p-pass` → backup: `p-bill`
- `p-bill` → backup: `p-tandoor`
- `p-wok` → no backup (single agent)

**How Backups Work:**

1. **Primary printer available**: Job prints to primary printer
2. **Primary printer offline**: Job automatically routes to backup printer
3. **Dashboard shows**: "PRINTED ON" column displays which printer actually received the job

**Example:**
- Order for Tandoor station (primary: `p-tandoor`)
- If `p-tandoor` is offline or busy
- Job automatically prints to `p-bill` (backup)
- Dashboard shows: Station=Tandoor, Printed On=Bill

**Benefits:**
- No orders lost if a printer fails
- Kitchen staff still receives orders
- Clear visibility in dashboard of backup usage
- Automatic recovery when primary printer comes back online

### Agent Architecture

**Front-Till Agent** (`dev-0001`)
- Manages: Tandoor, Pass, Bill printers
- Config: `C:\Users\Administrator\palate-agent\front\agent.json`

**Back-Till Agent** (`dev-0002`)
- Manages: Wok printer
- Config: `C:\Users\Administrator\palate-agent\back\agent.json`

Each agent:
- Polls server every few seconds for new jobs
- Works offline and syncs when reconnected
- Stores job ledger locally
- Handles printer health checks and failover

## Connecting Real Printers

See [REAL_PRINTER_SETUP.md](REAL_PRINTER_SETUP.md) for detailed instructions.

### Quick Overview

1. **Find your printer's IP address**
   - Check printer settings menu
   - Or use Windows printer properties

2. **Update agent configuration**

Edit `C:\Users\Administrator\palate-agent\front\agent.json`:

```json
{
    "server": "http://127.0.0.1:8080",
    "agent_name": "front-till",
    "printers": [
        {
            "id": "p-tandoor",
            "name": "Tandoor",
            "kind": "network",
            "address": "192.168.1.100:9100",
            "width": 80,
            "backup": "p-bill"
        },
        {
            "id": "p-pass",
            "name": "Pass",
            "kind": "network",
            "address": "192.168.1.101:9100",
            "width": 58,
            "backup": "p-bill"
        },
        {
            "id": "p-bill",
            "name": "Bill",
            "kind": "network",
            "address": "192.168.1.102:9100",
            "width": 80,
            "backup": "p-tandoor"
        }
    ]
}
```

**Key fields:**
- `address`: Printer IP and port (usually `9100` for ESC/POS)
- `width`: Paper width in mm (80mm or 58mm)
- `backup`: ID of backup printer (must be on same agent)

3. **Test printer connection**

```powershell
Test-NetConnection -ComputerName 192.168.1.100 -Port 9100
```

Should return `TcpTestSucceeded: True`

4. **Restart the agent**

```powershell
# Stop current agent
Get-Process -Name palate-agent | Stop-Process

# Start with new config
.\bin\palate-agent.exe run -config C:\Users\Administrator\palate-agent\front\agent.json
```

5. **Verify in dashboard**

Open http://127.0.0.1:8080 and check that printers show status "up"

### Supported Printer Types

- **Network printers**: ESC/POS thermal printers with Ethernet (port 9100)
- **USB printers**: Can be shared as network printer in Windows
- **Serial printers**: Not directly supported (use USB-to-serial adapter + network sharing)

### Troubleshooting Real Printers

**Printer shows "unknown":**
- Check printer is powered on and connected to network
- Verify IP address and port with `Test-NetConnection`
- Check firewall isn't blocking port 9100
- Restart the agent to re-detect printers

**Jobs print to backup:**
- Primary printer may be offline or busy
- Check "PRINTED ON" column in dashboard
- Verify printer responds to ping
- Check printer paper and error lights

**Partial prints or garbage:**
- Verify printer is ESC/POS compatible
- Check paper width setting (80mm vs 58mm)
- Some printers need firmware updates for ESC/POS

## Project Structure

```
d:\Palate-Print-Agent\
├── bin\                           # Compiled executables
│   ├── palate-agent.exe          # Print agent
│   ├── mockpalate-stations.exe   # Mock server
│   └── fakeprinter.exe           # Fake printer for testing
├── cmd\                          # Main programs
├── agent\                        # Agent logic
├── stationserver\                # Station document parser
├── internal\                     # Internal packages
├── START_ALL.ps1                 # One-command startup
├── STOP_ALL.ps1                  # Stop all components
├── README.md                     # This file
└── REAL_PRINTER_SETUP.md         # Real printer guide
```

## Configuration Files

**Agent configs** (outside project):
```
C:\Users\Administrator\palate-agent\
├── front\
│   ├── agent.json          # Front-till configuration
│   ├── credential.bin      # Paired credentials
│   └── ledger.bin          # Job history
└── back\
    ├── agent.json          # Back-till configuration
    ├── credential.bin
    └── ledger.bin
```

## Development

### Build from source

```powershell
go build -o bin/palate-agent.exe ./cmd/palate-agent
go build -o bin/mockpalate-stations.exe ./cmd/mockpalate-stations
go build -o bin/fakeprinter.exe ./cmd/fakeprinter
```

### Run tests

```powershell
go test ./...
```

### Manual startup (for development)

See individual component commands in `START_ALL.ps1` for reference.

## License

See LICENSE file for details.

## Support

For issues or questions:
1. Check dashboard for printer/agent status
2. Review agent logs (displayed in terminal)
3. Verify network connectivity to printers
4. Check `REAL_PRINTER_SETUP.md` for printer-specific issues
