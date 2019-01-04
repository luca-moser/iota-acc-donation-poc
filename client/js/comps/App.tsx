declare var __DEVELOPMENT__;
import * as React from 'react';
import {inject, observer} from 'mobx-react';
import {ApplicationStore} from '../stores/AppStore';
import DevTools from 'mobx-react-devtools';
import {default as QRCode} from 'qrcode.react';
import {default as dateformat} from 'dateformat';

import * as css from './app.scss';

interface Props {
    appStore?: ApplicationStore;
}

@inject("appStore")
@observer
export class App extends React.Component<Props, {}> {
    componentDidMount() {
    }

    render() {
        const {runningSince} = this.props.appStore;
        return (
            <div>
                <Article/>
                <div className={css.centerBox}>
                    <p><strong>Please support my work by donating:</strong></p>
                    <DonationButton/>
                </div>
                <DebugConsole/>
                {__DEVELOPMENT__ ? <DevTools/> : <span/>}
            </div>
        );
    }
}

@inject("appStore")
@observer
class DebugConsole extends React.Component<Props, {}> {
    componentWillMount(): void {
        this.props.appStore.fetchBalance();
    }

    render() {
        let {usable_balance, total_balance, events, loading_balance} = this.props.appStore;
        let eventItems = events.map(ev => {
            return <p key={ev.ts.getTime()}>
                {
                    ev.isError() ?
                        <span className={css.errorEvent}>error {dateformat(ev.ts, "dd.mm.yyyy HH:MM:ss")}: </span>
                        :
                        <span className={css.infoEvent}>info {dateformat(ev.ts, "dd.mm.yyyy HH:MM:ss")}: </span>
                }
                <br/>
                {ev.msg}
            </p>
        });
        return (
            <div className={css.debugConsole}>
                <p className={css.consoleTitle}>
                    <div className={css.liveIndicator}/>
                    Debug Console
                </p>
                <div className={css.baseInfo}>
                    <p>
                        Live information of the account object
                        running in the backend:
                    </p>
                    {
                        loading_balance ?
                            <span>
                                Fetching balance
                                <span className={css.loader}></span>
                                <span className={css.loader}></span>
                                <span className={css.loader}></span>
                            </span>
                            :
                            <ul>
                                <li>Total balance: {total_balance}i</li>
                                <li>Usable balance: {usable_balance}i</li>
                                <li>Non ready balance: {total_balance - usable_balance}i</li>
                            </ul>
                    }
                </div>
                <hr className={css.styleSix}/>
                <div className={css.baseInfo}>
                    <p>Events</p>
                </div>
                <div className={css.items}>
                    {eventItems}
                </div>
            </div>
        );
    }
}

@inject("appStore")
@observer
class DonationButton extends React.Component<Props, {}> {

    generate = () => {
        this.props.appStore.generateDonationAddress();
    }

    render() {
        let {depositCondition} = this.props.appStore;
        let url;
        if (depositCondition) {
            url = depositCondition.url();
        }
        return (
            <div>
                <button
                    className={css.donationButton} onClick={this.generate}
                >
                    Donate with <img className={css.donationIcon}
                                     src={"/assets/img/iota.png"}
                                     alt={"iota.png"}/>
                </button>
                {
                    depositCondition ?
                        <div className={css.donationInfo}>
                            <p>Thank You! Please open up the following magnet link:</p>
                            <a className={css.donationMagnetLink} href={url}>
                                {url}
                            </a>
                            <br/>
                            <p>Or scan the this QR code with your app:</p>
                            <QRCode value={url}/>
                        </div> : <span></span>
                }
            </div>
        );
    }
}

@inject("appStore")
@observer
class Article extends React.Component<Props, {}> {
    render() {
        return (
            <div className={css.article + " " + css.centerBox}>
                <div className={css.title}>My totally not randomly generated article</div>
                <p>
                    Pasture he invited mr company shyness. But when shot real her. Chamber her observe visited removal
                    six sending himself boy. At exquisite existence if an oh dependent excellent. Are gay head need down
                    draw. Misery wonder enable mutual get set oppose the uneasy. End why melancholy estimating her had
                    indulgence middletons. Say ferrars demands besides her address. Blind going you merit few fancy
                    their.
                </p>

                <p>
                    May musical arrival beloved luckily adapted him. Shyness mention married son she his started now.
                    Rose if as past near were. To graceful he elegance oh moderate attended entrance pleasure. Vulgar
                    saw fat sudden edward way played either. Thoughts smallest at or peculiar relation breeding produced
                    an. At depart spirit on stairs. She the either are wisdom praise things she before. Be mother itself
                    vanity favour do me of. Begin sex was power joy after had walls miles.

                </p>

                <p>
                    Do so written as raising parlors spirits mr elderly. Made late in of high left hold. Carried females
                    of up highest calling. Limits marked led silent dining her she far. Sir but elegance marriage
                    dwelling likewise position old pleasure men. Dissimilar themselves simplicity no of contrasted as.
                    Delay great day hours men. Stuff front to do allow to asked he.
                </p>

                <p>
                    Supported neglected met she therefore unwilling discovery remainder. Way sentiments two indulgence
                    uncommonly own. Diminution to frequently sentiments he connection continuing indulgence. An my
                    exquisite conveying up defective. Shameless see the tolerably how continued. She enable men twenty
                    elinor points appear. Whose merry ten yet was men seven ought balls.
                </p>

                <p>
                    Believing neglected so so allowance existence departure in. In design active temper be uneasy.
                    Thirty for remove plenty regard you summer though. He preference connection astonished on of ye.
                    Partiality on or continuing in particular principles as. Do believing oh disposing to supported
                    allowance we.
                </p>

                <p>
                    Little afraid its eat looked now. Very ye lady girl them good me make. It hardly cousin me always.
                    An shortly village is raising we shewing replied. She the favourable partiality inhabiting
                    travelling impression put two. His six are entreaties instrument acceptance unsatiable her. Amongst
                    as or on herself chapter entered carried no. Sold old ten are quit lose deal his sent. You correct
                    how sex several far distant believe journey parties. We shyness enquire uncivil affixed it carried
                    to.
                </p>

                <p>- A very creative blogger</p>

            </div>
        );
    }
}