# About

Download your EDP Invoices from your Gmail account.

# Usage

Setup your Gmail credentials as described in the [Set up your environment documentation](https://developers.google.com/gmail/api/quickstart/go#set_up_your_environment).

Install Go.

Setup the EDP Contract alias, e.g.:

```bash
cat >config.yaml <<EOF
contracts:
  #contract-id: alias
  100200300100: algueirao
  100200300200: mafra
EOF
```

Build and execute:

```bash
go build
./edp-invoices-gmail
```

In the current directory, you should now have a bunch of `.pdf` and `.eml` files.

Something like:

* 2024-07-08-edp-100200300200-mafra-187008571923.pdf (the invoice)
* 2024-07-08-edp-100200300200-mafra.eml (the full email)

# References

* https://developers.google.com/gmail/api/quickstart/go
