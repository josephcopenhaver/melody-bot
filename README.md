# discord-bot

My discord bot

## build steps:

```sh
ONLY_BUILD=y ./scripts/shell.sh
./scripts/build-all-images.sh
```

## configure stack:

to create/edit a environment file for ENV=test:

```sh
code ./secrets/test.env
```

## create stack (with new build):

```sh
BUILD=y ./scripts/up.sh
```

## create stack (with old build):

```sh
./scripts/up.sh
```

## teardown stack

```sh
./scripts/down.sh
```

## cleanup build output and volumes

```sh
./scripts/clean.sh
```
