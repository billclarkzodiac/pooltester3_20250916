// Simplified JavaScript - Just UI logic, no business rules

class PoolController {
    constructor() {
        this.powerLevels = [];
        this.sanitizerStates = {};
        this.loadPowerLevels();
        this.startStatePolling();
    }

    // Load power levels from server (single source of truth)
    async loadPowerLevels() {
        try {
            const response = await fetch('/api/power-levels');
            this.powerLevels = await response.json();
            this.renderPowerButtons();
        } catch (error) {
            console.error('Failed to load power levels:', error);
        }
    }

    // Render buttons dynamically from server-defined levels
    renderPowerButtons() {
        const container = document.getElementById('power-buttons');
        container.innerHTML = '';

        this.powerLevels.forEach(level => {
            const button = document.createElement('button');
            button.className = `btn btn-${level.color}`;
            button.id = `btn-${level.percentage}`;
            button.textContent = level.name;
            button.onclick = () => this.sendCommand('set_power', level.percentage);
            container.appendChild(button);
        });
    }

    // Simple command sender - no validation (server handles that)
    async sendCommand(action, value = null, duration = null) {
        const command = {
            action: action,
            value: value,
            duration: duration,
            client_id: 'web-ui',
            serial: this.getActiveSanitizerSerial() // Simple getter
        };

        try {
            const response = await fetch('/api/sanitizer/command', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(command)
            });

            const result = await response.json();
            this.showMessage(result.success ? result.message : result.error,
                result.success ? 'success' : 'error');
        } catch (error) {
            this.showMessage('Network error: ' + error.message, 'error');
        }
    }

    // Poll server for state updates
    async startStatePolling() {
        setInterval(async () => {
            try {
                const response = await fetch('/api/sanitizer/states');
                this.sanitizerStates = await response.json();
                this.updateUI();
            } catch (error) {
                console.error('Failed to fetch states:', error);
            }
        }, 1000); // 1 second polling
    }

    // Update UI based on server state
    updateUI() {
        Object.values(this.sanitizerStates).forEach(state => {
            this.updateDeviceCard(state);
            this.updateSliderPosition(state);
            this.highlightActiveButton(state.current_output);
        });
    }

    // Emergency stop - simple call, server handles logic
    async emergencyStop() {
        if (confirm('EMERGENCY STOP ALL SANITIZERS?')) {
            await fetch('/api/emergency-stop', { method: 'POST' });
        }
    }
}

// Initialize when page loads
document.addEventListener('DOMContentLoaded', () => {
    window.poolController = new PoolController();
});