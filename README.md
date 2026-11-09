# aigifmaker-go

Turn text into animated GIFs with AI GIF Maker. Native GIF generation — no video conversion. Type what you want and download your GIF in about a minute.

This is the official Go SDK and CLI for [AI GIF Maker](https://aigifmaker.ai/?utm_source=go&utm_medium=package): send a prompt, get back the URL of a real animated GIF, ready to drop into a chat, a README or your own app.

```go
url, err := aigifmaker.Generate(ctx, "a cat dancing in the rain, pixel art")
```

One call submits the prompt, waits for the GIF and returns its URL.

## Install

```bash
go get github.com/hanshs474/aigifmaker-go
```

Standard library only. The package name is `aigifmaker`.

The command-line tool:

```bash
go install github.com/hanshs474/aigifmaker-go/cmd/aigifmaker@latest
aigifmaker "a cat dancing in the rain, pixel art"   # prints the GIF URL
aigifmaker credits                                  # prints the credits left
```

## API key

Sign in, create a key at https://aigifmaker.ai/settings/apikeys, then export it:

```bash
export AIGIFMAKER_API_KEY=sk-...
```

Each GIF costs 1 credit on your AI GIF Maker account, the same as on the website. New accounts get one free GIF (it carries a small aigifmaker.ai badge). Failed generations are refunded. Credits and plans are on the [pricing](https://aigifmaker.ai/pricing?utm_source=go&utm_medium=package) page.

## Lower level

```go
c := aigifmaker.New("")                  // "" reads AIGIFMAKER_API_KEY
id, err := c.Submit(ctx, "a rocket launching, retro cartoon")
st, err := c.Status(ctx, id)            // st.Status: pending | processing | success | failed
url := st.URL()                         // the GIF, once st.Status == "success"
left, err := c.Credits(ctx)             // credits left on the account, free to call
```

`Client` fields: `APIKey`, `BaseURL` (default `https://aigifmaker.ai`), `PollEvery` (3 seconds), `Timeout` (15 minutes in total), `HTTP`.

On an account that has not bought credits yet, a GIF can wait in a queue first: `Status` then reports `processing` with `Queued` set and `QueueEta` in seconds. `Generate` and `Wait` keep polling through it.

## Errors worth catching

- `aigifmaker.ErrAuth`: no key, or the key is wrong or deleted
- `aigifmaker.ErrQuota`: the account is out of credits
- `aigifmaker.ErrRejected`: moderation refused the prompt, or the account is cooling down after a refusal; reword rather than retry
- `aigifmaker.ErrFailed`: the GIF was accepted and then failed; `*aigifmaker.Error` carries the message, and the credit comes back
- `aigifmaker.ErrTimeout`: the wait ran out; the GIF may still finish in your history on the site
- `aigifmaker.ErrAPI`: anything else, with the service's message verbatim

Test with `errors.Is(err, aigifmaker.ErrAuth)`. The CLI exits with 0 ok, 1 failure, 2 usage, 3 key problem, 4 out of credits.

## Support

support@aigifmaker.ai

MIT licensed.
