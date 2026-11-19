#!/usr/bin/env powershell
# Complete One-Command Startup Script for Palate Print Agent
# Starts everything: fake printers, server, and agents

Write-Host "`n=== PALATE PRINT AGENT - ONE-COMMAND STARTUP ===" -ForegroundColor Cyan
Write-Host "Starting all components...`n" -ForegroundColor Gray

# Navigate to project directory
Set-Location d:\Palate-Print-Agent

# Clean up any old processes
Write-Host "Cleaning up old processes..." -ForegroundColor Gray
Get-Process | Where-Object { $_.Name -match 'fakeprinter|mockpalate|palate-agent' } | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

# Clean up old PowerShell jobs
Get-Job | Stop-Job -ErrorAction SilentlyContinue
Get-Job | Remove-Job -ErrorAction SilentlyContinue

Write-Host "Ready to start fresh`n" -ForegroundColor Gray

# Step 1: Start Fake Printers
Write-Host "[1/4] Starting Fake Printers..." -ForegroundColor Green
Start-Job -Name "printer-tandoor" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\fakeprinter.exe -name p-tandoor -addr 127.0.0.1:19100 -control 127.0.0.1:19190 
} | Out-Null
Start-Sleep -Milliseconds 500
Start-Job -Name "printer-pass" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\fakeprinter.exe -name p-pass -addr 127.0.0.1:19101 -control 127.0.0.1:19191 
} | Out-Null
Start-Sleep -Milliseconds 500
Start-Job -Name "printer-wok" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\fakeprinter.exe -name p-wok -addr 127.0.0.1:19102 -control 127.0.0.1:19192 
} | Out-Null
Start-Sleep -Milliseconds 500
Start-Job -Name "printer-bill" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\fakeprinter.exe -name p-bill -addr 127.0.0.1:19103 -control 127.0.0.1:19193 
} | Out-Null
Start-Sleep -Seconds 2
Write-Host "  * Started 4 fake printers" -ForegroundColor Gray

# Step 2: Start Mock Palate Server
Write-Host "[2/4] Starting Mock Palate Server..." -ForegroundColor Green
Start-Job -Name "mock-server" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\mockpalate-stations.exe -agent-port 8080 -station-port 8081 -stations "tandoor=p-tandoor,wok=p-wok,pass=p-pass" -bill-printer p-bill -currency "Rs " -printer-control "p-tandoor=http://127.0.0.1:19190,p-pass=http://127.0.0.1:19191,p-wok=http://127.0.0.1:19192,p-bill=http://127.0.0.1:19193"
} | Out-Null
Start-Sleep -Seconds 4
Write-Host "  * Server started on http://127.0.0.1:8080" -ForegroundColor Gray

# Step 3: Generate Pairing Codes
Write-Host "[3/4] Generating Pairing Codes..." -ForegroundColor Green
$frontCodeResponse = Invoke-WebRequest -Uri "http://127.0.0.1:8080/mock/pairing-codes" -Method POST -Body '{"restaurant_id":"demo"}' -ContentType "application/json" -UseBasicParsing
$frontCode = ($frontCodeResponse.Content | ConvertFrom-Json).code
Start-Sleep -Milliseconds 500
$backCodeResponse = Invoke-WebRequest -Uri "http://127.0.0.1:8080/mock/pairing-codes" -Method POST -Body '{"restaurant_id":"demo"}' -ContentType "application/json" -UseBasicParsing
$backCode = ($backCodeResponse.Content | ConvertFrom-Json).code
Write-Host "  * Front Till Code: $frontCode" -ForegroundColor Cyan
Write-Host "  * Back Till Code:  $backCode" -ForegroundColor Cyan

# Step 4: Pair and Start Agents
Write-Host "[4/4] Pairing and Starting Agents..." -ForegroundColor Green

# Delete old credentials
if (Test-Path "C:\Users\Administrator\palate-agent\front\credential.bin") {
    Remove-Item "C:\Users\Administrator\palate-agent\front\credential.bin" -Force
}
if (Test-Path "C:\Users\Administrator\palate-agent\back\credential.bin") {
    Remove-Item "C:\Users\Administrator\palate-agent\back\credential.bin" -Force
}

# Pair Front Till
Write-Host "  Pairing front-till..." -ForegroundColor Gray -NoNewline
Start-Process -FilePath ".\bin\palate-agent.exe" -ArgumentList "pair","-config","C:\Users\Administrator\palate-agent\front\agent.json","-code",$frontCode -NoNewWindow -Wait
Write-Host " Done" -ForegroundColor Green

# Pair Back Till
Write-Host "  Pairing back-till..." -ForegroundColor Gray -NoNewline
Start-Process -FilePath ".\bin\palate-agent.exe" -ArgumentList "pair","-config","C:\Users\Administrator\palate-agent\back\agent.json","-code",$backCode -NoNewWindow -Wait
Write-Host " Done" -ForegroundColor Green

# Start Front Till Agent
Start-Job -Name "agent-front" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\palate-agent.exe run -config C:\Users\Administrator\palate-agent\front\agent.json
} | Out-Null

# Start Back Till Agent
Start-Job -Name "agent-back" -ScriptBlock { 
    Set-Location d:\Palate-Print-Agent
    .\bin\palate-agent.exe run -config C:\Users\Administrator\palate-agent\back\agent.json
} | Out-Null

Start-Sleep -Seconds 3
Write-Host "  * Started front-till agent (Tandoor, Pass, Bill)" -ForegroundColor Gray
Write-Host "  * Started back-till agent (Wok)" -ForegroundColor Gray
Write-Host "  * Backup printers configured: all stations -> Bill" -ForegroundColor Gray

# Summary
Write-Host "`n=== SYSTEM RUNNING ===" -ForegroundColor Green
Write-Host "All components started successfully!`n"
Write-Host "Dashboard:  " -NoNewline; Write-Host "http://127.0.0.1:8080" -ForegroundColor Cyan
Write-Host "Order UI:   " -NoNewline; Write-Host "http://127.0.0.1:8081" -ForegroundColor Cyan
Write-Host "`nTo view background jobs: " -NoNewline; Write-Host "Get-Job" -ForegroundColor Yellow
Write-Host "To stop all jobs:        " -NoNewline; Write-Host "Get-Job | Stop-Job; Get-Job | Remove-Job" -ForegroundColor Yellow
Write-Host "`nOpening dashboard..." -ForegroundColor Gray
Start-Sleep -Seconds 1
Start-Process "http://127.0.0.1:8080"

Write-Host "`nReady to accept orders at http://127.0.0.1:8081`n" -ForegroundColor Green
