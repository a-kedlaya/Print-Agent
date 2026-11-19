# Real Printer Setup Guide

Complete guide for connecting real ESC/POS thermal printers to the Palate Print Agent system.

## Prerequisites

- Windows PC with network connectivity
- ESC/POS thermal receipt printers (80mm or 58mm)
- Printers connected to same network as the PC
- Printer IP addresses configured (static recommended)

## Supported Printers

**Tested Brands:**
- Epson TM-series (TM-T20, TM-T82, TM-T88)
- Star TSP series
- Citizen CT-S series
- Bixolon SRP series
- Generic ESC/POS compatible printers

**Requirements:**
- ESC/POS protocol support
- Network interface (Ethernet) OR USB + Windows printer sharing
- Port 9100 (raw socket printing)

## Step 1: Prepare Your Printers

### For Network Printers

1. **Connect printer to network**
   - Plug Ethernet cable into printer and router/switch
   - Power on printer

2. **Get printer IP address**
   
   **Method A: Print configuration page**
   - Most printers have a button combination to print network settings
   - Example (Epson TM-T88): Hold FEED button while powering on
   - Look for "IP Address" on printout

   **Method B: Check router admin panel**
   - Login to router (usually 192.168.1.1 or 192.168.0.1)
   - Look for connected devices / DHCP leases
   - Find printer by MAC address or device name

3. **Set static IP (recommended)**
   - Access printer web interface: http://[printer-ip]
   - Or use printer utility software (varies by brand)
   - Set static IP in same subnet as PC
   - Example: PC is 192.168.1.50, set printer to 192.168.1.100

### For USB Printers

USB printers need to be shared as network printers:

1. **Install USB printer in Windows**
   - Control Panel → Devices and Printers → Add a printer
   - Follow manufacturer instructions

2. **Share the printer**
   - Right-click printer → Printer properties → Sharing tab
   - Enable "Share this printer"
   - Note the share name

3. **Create network port**
   - This requires third-party software or Windows print server
   - Alternatively: use a USB print server device (hardware solution)

**Note**: Direct USB connection requires the agent to run on the same PC as the printer. Network printers are recommended for flexibility.

## Step 2: Test Printer Connectivity

### Test A: Ping the printer

```powershell
ping 192.168.1.100
```

Expected: Replies from printer (not timeout)

### Test B: Test port 9100

```powershell
Test-NetConnection -ComputerName 192.168.1.100 -Port 9100
```

Expected output:
```
TcpTestSucceeded : True
```

If `TcpTestSucceeded : False`:
- Printer may not support raw socket printing
- Check printer settings for "RAW TCP" or "Port 9100" mode
- Firewall may be blocking (check Windows Firewall and printer firewall)

### Test C: Send test print

```powershell
# Create a test ESC/POS command
$escInit = [byte[]](0x1B, 0x40)  # Initialize printer
$text = [System.Text.Encoding]::UTF8.GetBytes("Test from Palate Print Agent`n`n`n")
$cut = [byte[]](0x1D, 0x56, 0x00)  # Cut paper

$data = $escInit + $text + $cut

# Send to printer
$client = New-Object System.Net.Sockets.TcpClient("192.168.1.100", 9100)
$stream = $client.GetStream()
$stream.Write($data, 0, $data.Length)
$stream.Close()
$client.Close()

Write-Host "Test sent! Check printer."
```

Expected: Printer prints "Test from Palate Print Agent" and cuts paper.

## Step 3: Configure Agent

### Find your agent config files

**Front-till agent**: `C:\Users\Administrator\palate-agent\front\agent.json`
**Back-till agent**: `C:\Users\Administrator\palate-agent\back\agent.json`

### Example Configuration

```json
{
    "server": "http://127.0.0.1:8080",
    "agent_name": "front-till",
    "printers": [
        {
            "id": "p-tandoor",
            "name": "Tandoor Kitchen",
            "kind": "network",
            "address": "192.168.1.100:9100",
            "width": 80,
            "backup": "p-bill"
        },
        {
            "id": "p-pass",
            "name": "Pass Station",
            "kind": "network",
            "address": "192.168.1.101:9100",
            "width": 58,
            "backup": "p-bill"
        },
        {
            "id": "p-bill",
            "name": "Customer Bill",
            "kind": "network",
            "address": "192.168.1.102:9100",
            "width": 80,
            "backup": "p-tandoor"
        }
    ]
}
```

### Configuration Fields

| Field | Description | Example |
|-------|-------------|---------|
| `id` | Unique printer identifier | `"p-tandoor"` |
| `name` | Human-readable name | `"Tandoor Kitchen"` |
| `kind` | Connection type | `"network"` (only supported) |
| `address` | IP:Port | `"192.168.1.100:9100"` |
| `width` | Paper width in mm | `80` or `58` |
| `backup` | Backup printer ID (optional) | `"p-bill"` |

### Important Notes

1. **Printer IDs must match server configuration**
   - The mock server expects: `p-tandoor`, `p-pass`, `p-wok`, `p-bill`
   - Don't change these IDs unless you also update the server config

2. **Backup printer must be on same agent**
   - ✅ Valid: `p-tandoor` → backup: `p-bill` (both on front-till)
   - ❌ Invalid: `p-wok` → backup: `p-bill` (different agents)

3. **Port 9100 is standard**
   - Most ESC/POS printers use port 9100
   - Check printer documentation if connection fails

4. **Paper width affects formatting**
   - 80mm printers: More columns, better for bills
   - 58mm printers: Narrow receipts, compact KOTs

## Step 4: Stop Fake Printers

Before using real printers, stop the fake printers:

```powershell
# Stop all fake printers
Get-Process -Name fakeprinter | Stop-Process

# Or stop entire system and restart without fake printers
.\STOP_ALL.ps1
```

## Step 5: Start Mock Server Only

Instead of `START_ALL.ps1` (which starts fake printers), start only the server:

```powershell
# Start mock server
.\bin\mockpalate-stations.exe `
  -agent-port 8080 `
  -station-port 8081 `
  -stations "tandoor=p-tandoor,wok=p-wok,pass=p-pass" `
  -bill-printer p-bill `
  -currency "Rs "
```

**Leave this terminal open** - the server runs in foreground.

## Step 6: Pair and Start Agents

### Get pairing codes from dashboard

1. Open http://127.0.0.1:8080
2. Look for "Tills" section
3. Note the pairing codes (or generate new ones)

### Delete old credentials

```powershell
Remove-Item C:\Users\Administrator\palate-agent\front\credential.bin -Force
Remove-Item C:\Users\Administrator\palate-agent\back\credential.bin -Force
```

### Pair front-till agent

Open a **new PowerShell terminal**:

```powershell
cd d:\Palate-Print-Agent

# Pair (use code from dashboard)
.\bin\palate-agent.exe pair `
  -config C:\Users\Administrator\palate-agent\front\agent.json `
  -code XXXX-XXXX

# Run
.\bin\palate-agent.exe run `
  -config C:\Users\Administrator\palate-agent\front\agent.json
```

**Leave this terminal open** - the agent runs in foreground.

### Pair back-till agent

Open **another PowerShell terminal**:

```powershell
cd d:\Palate-Print-Agent

# Pair (use different code from dashboard)
.\bin\palate-agent.exe pair `
  -config C:\Users\Administrator\palate-agent\back\agent.json `
  -code YYYY-YYYY

# Run
.\bin\palate-agent.exe run `
  -config C:\Users\Administrator\palate-agent\back\agent.json
```

**Leave this terminal open** - the agent runs in foreground.

## Step 7: Verify System

### Check Dashboard

Open http://127.0.0.1:8080

**Expected:**
- **Tills**: 2 tills showing "polling" status
- **Printers**: All printers showing status "up" (not "unknown")

**If printers show "unknown":**
1. Check printer is powered on
2. Verify IP address with `Test-NetConnection`
3. Check network cable is connected
4. Restart agent to re-detect printers

### Submit Test Order

1. Open http://127.0.0.1:8081
2. Enter test order:
   ```
   Tandoor: 1x Test Item : 100
   Order: TEST / T1
   ```
3. Click "Send Order to All Stations + Print Bill"

**Expected:**
- Dashboard shows 2 jobs (1 KOT + 1 Bill)
- Both jobs show "printed" status
- Tandoor printer prints the KOT
- Bill printer prints the customer bill

## Troubleshooting

### Printer shows "unknown" status

**Cause**: Agent can't reach printer

**Solutions**:
1. Verify printer IP: `ping 192.168.1.100`
2. Test port: `Test-NetConnection -ComputerName 192.168.1.100 -Port 9100`
3. Check firewall (Windows Firewall or printer firewall)
4. Verify printer supports ESC/POS and port 9100
5. Check printer network cable is connected
6. Restart agent to re-detect

### Jobs print to backup printer

**Cause**: Primary printer unavailable or slow to respond

**Check dashboard**:
- "PRINTED ON" column shows which printer received job
- If showing backup, primary printer had an issue

**Solutions**:
1. Check primary printer status (power, paper, errors)
2. Verify primary printer responds: `Test-NetConnection`
3. Check printer error lights or display
4. Restart printer if hung
5. Agent will automatically use primary when it recovers

### Garbage or partial prints

**Cause**: Wrong ESC/POS dialect or paper width

**Solutions**:
1. Check paper width setting matches reality:
   - Measure paper: 80mm or 58mm?
   - Update `"width"` in agent config
2. Some printers need initialization:
   - Check printer manual for ESC/POS mode settings
   - May need firmware update
3. Try different ESC/POS commands:
   - Some printers use Star or legacy ESC/POS
   - Contact printer manufacturer for protocol details

### No print output

**Cause**: Printer not receiving data or wrong port

**Solutions**:
1. Verify port 9100: `Test-NetConnection`
2. Try test print from Step 2 (PowerShell test)
3. Check printer is in "raw TCP" or "socket" mode
4. Some printers require mode switch:
   - Check printer settings menu
   - Enable "ESC/POS" or "Raw TCP" mode
   - Port 9100 must be enabled

### Paper not cutting

**Cause**: Printer doesn't have auto-cutter or wrong ESC/POS command

**Solutions**:
1. Check if printer has built-in cutter
2. Manual cut may be needed for some printers
3. Different ESC/POS cut commands:
   - Full cut: `0x1D 0x56 0x00`
   - Partial cut: `0x1D 0x56 0x01`
   - Check printer manual for correct command

### Agent can't pair

**Cause**: Pairing code expired or server not running

**Solutions**:
1. Check server is running: http://127.0.0.1:8080
2. Generate new pairing code (codes expire after 10 minutes)
3. Use code immediately after generation
4. Check agent can reach server: `Test-NetConnection -ComputerName 127.0.0.1 -Port 8080`

## Multiple Agent Setup

For larger restaurants, you can run agents on multiple PCs:

### Example: 3 PCs

**PC 1 (Front Counter)**
- Runs mock server
- Agent with Bill printer

**PC 2 (Kitchen Area)**
- Agent with Tandoor + Wok printers

**PC 3 (Pass Station)**
- Agent with Pass printer

### Configuration

Change `"server"` in agent.json to point to PC 1's IP:

```json
{
    "server": "http://192.168.1.50:8080",
    "agent_name": "kitchen-agent",
    "printers": [...]
}
```

**Firewall**: Ensure port 8080 is open on PC 1 for other PCs to connect.

## Production Considerations

### 1. Static IP Addresses

Set static IPs for:
- All printers
- Server PC (if running agents on other PCs)

This prevents IP changes from breaking connectivity.

### 2. Network Quality

- Use wired Ethernet (not WiFi) for printers
- WiFi can cause delays and dropped jobs
- Keep printers and PC on same network switch if possible

### 3. Backup Printers

Configure backup printers appropriately:
- Important stations (Tandoor, Wok) → backup to Bill printer
- Bill printer → backup to a station printer
- Ensures no orders lost during printer failures

### 4. Monitoring

Check dashboard regularly:
- Verify all printers show "up"
- Monitor agent status (polling)
- Check for queued jobs (indicates printer issues)

### 5. Maintenance

- Keep printer paper stocked
- Clean printer heads regularly
- Update printer firmware if available
- Test backup failover monthly

## Support

For hardware-specific issues, consult:
1. Printer manufacturer documentation
2. ESC/POS command reference for your printer model
3. Network administrator for connectivity issues

For software issues, check:
1. Dashboard at http://127.0.0.1:8080
2. Agent terminal logs
3. README.md for system overview
