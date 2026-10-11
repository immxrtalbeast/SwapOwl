# tests/stability

Прогон стабильности под генератором (критерий 4 шкалы): `cmd/api` на отдельной базе и `cmd/simulator run`
на ×600. Посреди прогона — 50 одновременных заказов (дважды), перезапуск `cmd/api` (SIGTERM, затем kill -9),
сброс стенда (под генератором и два повтора с сравнением состояния).

    export TEST_DATABASE_URL='postgres://ci:ci@localhost:5432/ci?sslmode=disable'
    STABILITY_MINUTES=2 go test -race -p 1 -timeout 20m ./tests/stability/...   # короткий
    STABILITY_RUN=1 go test -race -p 1 -timeout 20m ./tests/stability/...       # 10 минут

Без `STABILITY_MINUTES`/`STABILITY_RUN` тест ничего не делает (не t.Skip: CI считает пропуски ошибкой), чтобы не удлинять общий `go test ./...`.
Проверки: ни одного 5xx; остатки не отрицательны и сходятся с журналом движений; нет двойной продажи в залпе;
данные и время стенда после перезапуска; одинаковое состояние после сброса.
