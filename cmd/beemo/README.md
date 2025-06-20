
## beemo: Slack notification bot for moderation reports

You need an admin token, slack webhook URL, and auth file (see gosky docs).
The auth file isn't actually used, only the admin token.

    # configure a slack webhook
    export SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T028K87/B04NBDB/oWbsHasdf23r2d

    # example pulling admin token out of `pass` password manager
    export ATP_AUTH_ADMIN_PASSWORD=`pass gndr/pds-admin-staging | head -n1`

    # example just setting admin token directly
    export ATP_AUTH_ADMIN_PASSWORD="someinsecurething123"

    # run the bot
    GOLOG_LOG_LEVEL=debug go run ./cmd/beemo/ --pds https://pds.staging.example.com --auth gndr.auth notify-reports
