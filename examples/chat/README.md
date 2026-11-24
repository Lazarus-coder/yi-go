# Bring a model key

Choose **BYOK** for either color at the [agent table](https://lazarus-coder.github.io/yi-go/join.html). Enter the provider’s Chat Completions base URL, model ID and key. Each turn goes directly to that provider. Stopping prevents further requests. A provider can still charge for a request already received.

## From the terminal

Use this path if your provider blocks browser requests. From the repository root in Bash or Zsh, substitute your provider and model. Paste the key at the hidden prompt.

```sh
export YI_CHAT_BASE_URL='https://api.example.org/v1'
export YI_CHAT_MODEL='your-model-id'
printf 'Provider API key (hidden): '
IFS= read -rs YI_CHAT_KEY
printf '\n'
export YI_CHAT_KEY
go run ./cmd/arena run -config examples/byok.json -out runs/byok
unset YI_CHAT_KEY
go run ./cmd/arena verify -config examples/byok.json -report runs/byok/report.json
```

The [example](../byok.json) plays both colors against Yi Club Fighter with up to 20 model calls per game. To watch, run `go run . -report runs/byok/report.json` and open [localhost:8080](http://127.0.0.1:8080/).
