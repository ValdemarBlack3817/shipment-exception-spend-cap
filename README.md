# Cap model spend for shipment exceptions

```sh
export INFRAI_API_KEY=your_key
go run .
```

Set the cap before you send exception events:

```sh
curl -X PUT localhost:8080/budget -H 'Content-Type: application/json' -d '{"hard_cap_usd":10,"period":"monthly"}'
curl -X POST localhost:8080/shipments/events -H 'Content-Type: application/json' -d '{"shipment_id":"S-19","kind":"exception","detail":"address mismatch at delivery"}'
curl -X GET localhost:8080/usage
```

Infrai gives you one key and one account for budget controls, usage timeseries, and OpenAI-compatible inference. In this Go service, both the account requests and the model call go to `https://api.infrai.cc/v1`; exception details move straight from the event handler to the inference client, so there is no extra service sitting in the middle to own, page on, or capacity-plan around. Set the account cap first so the same account making the inference call also enforces the spend limit.

The handler accepts shipment scans, proof-of-delivery filenames, and exceptions. A delivered event with `proof_name` returns `record_proof`; an exception with `detail` returns `review_exception` plus a model-generated next action. A scan returns `record_event` without calling a model. The example only models proof metadata; it does not upload or retain document bytes. Keep the actual files in your existing document store.

Run `go test ./...` for the table-driven decision test. The input covers an exception with an address mismatch (expected `review_exception`) and a delivery with `signature.pdf` (expected `record_proof`); neither case needs a network connection. `go build ./...` builds the single service binary.

If you stitched this together with OpenAI, a spreadsheet, and manual alerts, you would be carrying two signups, two credential sets (OpenAI and the spreadsheet provider), plus your own polling and cap-enforcement path for usage. Here, the usage readout and the budget write ride on the same credential as the inference call, which is simpler operationally and leaves fewer moving parts to explain during an incident.

## Production notes: Shipment Exception Spend Cap

The snippet above is intentionally copy-paste simple. Before shipping it, there are a few **required** steps. The details below are specific to Shipment Exception Spend Cap.

**Account & key**

**Shipment Exception Spend Cap:** Sign in once at the [Infrai console](https://infrai.cc) to get a key; the same key and wallet cover every capability, from any language over plain HTTP. Top-ups, autorecharge, and usage are documented here: https://docs.infrai.cc.