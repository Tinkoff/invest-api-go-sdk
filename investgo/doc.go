/*
Package investgo предоставляет инструменты для работы с Tinkoff InvestAPI.

#	Client

Сначала нужно заполнить investgo.Config, затем с помощью функции investgo.NewClient() создать клиента. У каждого клиента
есть свой конфиг, который привязывает его к определенному счету и токену. Если есть потребность использовать разные счета и токены, нужно
создавать разных клиентов. investgo.Client предоставляет функции-конcтрукторы для всех сервисов Tinkoff InvestAPI.

Подробнее смотрите в директории examples.
*/

package investgo
