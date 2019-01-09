import {observable, runInAction} from 'mobx';
import {default as axios} from 'axios';

const donationURI = "/account/donation-link";
const balanceURI = "/account/balance";

class DepCond {
    timeout_on: Date;
    multi_use: boolean = false;
    expected_amount: number = 0;
    address: string;

    url(): string {
        let time = Math.round(new Date(this.timeout_on).getTime() / 1000);
        let am = this.expected_amount ? this.expected_amount : 0;
        return `iota://${this.address}/?t=${time}&m=${this.multi_use}&am=${am}`;
    }
}

export const MsgType = {
    Stop: 0,
    Promotion: 1,
    Reattachment: 2,
    Sending: 3,
    Sent: 4,
    ReceivingDeposit: 5,
    ReceivedDeposit: 6,
    ReceivedMessage: 7,
    Error: 8,
    Balance: 9,
};

class WsMsg {
    msg_type: number;
    data: any;
    ts: string;
}

export const EventType = {
    Info: 0,
    Error: 1,
};

export class Event {
    ts: Date;
    type: number;
    msg: string;

    constructor(msg: string, ts: Date, type: number) {
        this.msg = msg;
        this.type = type;
        this.ts = new Date(ts);
    }

    isError(): boolean {
        return this.type == EventType.Error;
    }
}

export class ApplicationStore {
    @observable runningSince = 0;
    @observable depositCondition: DepCond = null;
    @observable usable_balance: number = 0;
    @observable total_balance: number = 0;
    @observable generating = false;
    @observable loading_balance = true;
    @observable events: Array<Event> = [];
    ws: WebSocket;
    timerID;

    constructor() {
        this.timerID = setInterval(() => {
            runInAction(this.updateTimer);
        }, 1000);
        this.connectWS();
    }

    connectWS = () => {
        this.ws = new WebSocket(`ws://${location.host}/account/live`);
        this.ws.onmessage = (e: MessageEvent) => {
            let obj: WsMsg = JSON.parse(e.data);
            let event;
            let now = new Date()
            let tail, bundle, msg, value;
            switch (obj.msg_type) {
                case MsgType.Error:
                    event = new Event(JSON.stringify(obj.data), now, EventType.Error);
                    break;
                case MsgType.Promotion:
                    tail = obj.data.promotion_tail_tx_hash;
                    bundle = obj.data.bundle_hash;
                    event = new Event(`promoted bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.Reattachment:
                    tail = obj.data.reattachment_tail_tx_hash;
                    bundle = obj.data.bundle_hash;
                    event = new Event(`reattached bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.ReceivingDeposit:
                    tail = obj.data[0].hash;
                    bundle = obj.data[0].bundle;
                    value = obj.data[0].value;
                    event = new Event(`receiving deposit ${value}i; bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.ReceivedDeposit:
                    tail = obj.data[0].hash;
                    bundle = obj.data[0].bundle;
                    value = obj.data[0].value;
                    event = new Event(`received deposit ${value}i; bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.ReceivedMessage:
                    tail = obj.data[0].hash;
                    bundle = obj.data[0].bundle;
                    event = new Event(`received message; bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.Sending:
                    tail = obj.data[0].hash;
                    bundle = obj.data[0].bundle;
                    event = new Event(`sending bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.Sent:
                    tail = obj.data[0].hash;
                    bundle = obj.data[0].bundle;
                    event = new Event(`sent bundle ${bundle} with tail ${tail}`, now, EventType.Info);
                    break;
                case MsgType.Balance:
                    event = new Event(`updated balance`, now, EventType.Info);
                    runInAction(() => {
                        this.usable_balance = obj.data.usable;
                        this.total_balance = obj.data.total;
                    });
                    break;
            }

            if (event) {
                // add the new event
                runInAction(() => {
                    this.events.push(event);
                });
            }
        }
    }

    updateTimer = () => {
        this.runningSince++;
    }

    fetchBalance = async () => {
        try {
            let res = await axios.get(balanceURI);
            runInAction(() => {
                this.usable_balance = res.data.usable;
                this.total_balance = res.data.total;
                this.loading_balance = false;
            });
        } catch (err) {
            console.error(err);
            this.fetchBalance();
            // TODO: do something
        }
    }

    generateDonationAddress = async () => {
        try {
            runInAction(() => {
                this.generating = true;
            });
            let res = await axios.get(donationURI);
            let depCond: DepCond = Object.assign(new DepCond(), res.data);
            runInAction(() => {
                this.generating = false;
                this.depositCondition = depCond;
            });
        } catch (err) {
            console.error(err);
            // TODO: do something
            runInAction(() => {
                this.generating = false;
            });
        }

    }
}

export var AppStoreInstance = new ApplicationStore();