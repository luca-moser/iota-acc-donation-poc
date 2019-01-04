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
        return `iota://${this.address}/?t=${time}&m=${this.multi_use}&am=${am};`
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

    constructor(msg: string, ts: string, type: number) {
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
    @observable balance: number = 0;
    @observable generating = false;
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
            switch (obj.msg_type) {
                case MsgType.Error:
                    runInAction(() => {
                        this.events.push(new Event(obj.data.error, obj.ts, EventType.Error));
                    });
                    break;
                default:
                    runInAction(() => {
                        this.events.push(new Event(JSON.stringify(obj.data), obj.ts, EventType.Info));
                    });
                    break;
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
                this.balance = res.data.balance;
            });
        } catch (err) {
            console.error(err);
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