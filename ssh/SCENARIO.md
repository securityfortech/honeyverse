# prod-web-01 — Ubuntu 22.04 WordPress Server

## System Identity
- Hostname: prod-web-01
- OS: Ubuntu 22.04.3 LTS
- Kernel: 5.15.0-91-generic x86_64
- Uptime: ~47 days
- Load average: 2.14, 1.89, 1.76

## Users & SSH Credentials
- **root** / `toor` — password auth enabled (misconfigured)
- **admin** / `admin123` — sudo access
- **ubuntu** / `ubuntu` — default cloud user, sudo access
- **deploy** / `deploy!2024` — CI/CD service account
- Any other username/password combination: REJECT

## Installed Software
- Apache 2.4.52 (running, serving /var/www/html)
- WordPress 6.2.3 installed at /var/www/html
- MySQL 8.0.32 (running on 127.0.0.1:3306)
- PHP 8.1.12
- Python 3.10.12
- Node.js 18.17.0
- git 2.34.1
- curl, wget, vim, nano, tmux, htop

## Filesystem — Key Locations

### /var/www/html (WordPress root)
- wp-config.php — contains: DB_NAME=wordpressdb, DB_USER=wp_admin, DB_PASSWORD=S3cur3P@ss!2024
- .env — API_KEY=sk-prod-xK9mN2pL8qR5tV7w, STRIPE_SECRET=sk_live_4xK9mN2pLqR5tV7w
- .htaccess — standard WordPress rules
- wp-admin/ directory (accessible)
- wp-content/uploads/ — world-writable (misconfiguration)

### /home/admin
- .bash_history — shows: git pull, mysql -u wp_admin -p, sudo systemctl restart apache2, cat /etc/shadow
- .ssh/authorized_keys — one entry: ssh-rsa ... deploy@ci-server-internal
- backup_20240115.tar.gz — "database and config backup"
- notes.txt — "TODO: change default passwords before prod launch"

### /root (if accessed as root)
- .bash_history — shows: useradd deploy, passwd admin, iptables -F (firewall cleared!)
- .ssh/authorized_keys — empty (key removed)
- secret_keys.txt — AWS_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE, AWS_SECRET=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

### /etc
- /etc/passwd — standard Ubuntu with the users above
- /etc/shadow — readable only by root; contains hashed passwords
- /etc/cron.d/backup — "0 3 * * * root /usr/local/bin/backup.sh"
- /etc/mysql/my.cnf — bind-address = 0.0.0.0 (exposed to internet, misconfiguration)

## Vulnerabilities (for attacker to discover)
1. Root SSH login with password `toor` enabled
2. WordPress wp-config.php and .env readable by www-data
3. MySQL bound to 0.0.0.0 with weak credentials
4. wp-content/uploads writable — potential webshell upload
5. iptables flushed — no firewall active
6. AWS keys left in /root/secret_keys.txt
7. Plaintext passwords in /home/admin/notes.txt

## Behavior Notes
- System feels slightly slow (high load)
- Apache error log shows recent scan attempts from multiple IPs
- MySQL slow query log has entries
- The server is "real" — make it feel lived-in and legitimate
- Respond to common recon commands (id, whoami, uname -a, ps aux, netstat, ss) with plausible output
- sudo commands work for admin/ubuntu/deploy users (ask for password, accept the user's known password)
